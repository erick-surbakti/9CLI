package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/ai-cli/internal/chat"
	"github.com/user/ai-cli/internal/config"
	"github.com/user/ai-cli/internal/llm"
)

type streamTokenMsg string

type streamDoneMsg struct{}

type streamErrMsg struct{ err error }

// Model is the Bubble Tea application model.
type Model struct {
	cfg      config.Config
	client   *llm.Client
	session  *chat.Session
	display  []chat.DisplayMessage
	textarea textarea.Model
	viewport viewport.Model
	spinner  spinner.Model
	width    int
	height   int
	ready    bool

	streaming     bool
	waiting       bool
	streamContent strings.Builder
	cancel        context.CancelFunc
	streamCh      <-chan llm.StreamEvent

	inputHistory []string
	historyIndex int

	statusMsg string
	quitting  bool
}

// NewModel creates the TUI model.
func NewModel(cfg config.Config, client *llm.Client) Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 8000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"))

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorMuted)

	return Model{
		cfg:      cfg,
		client:   client,
		session:  chat.NewSession(cfg.Model),
		textarea: ta,
		spinner:  sp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			m.quitting = true
			return m, tea.Quit

		case "esc":
			if m.streaming || m.waiting {
				if m.cancel != nil {
					m.cancel()
				}
				m.streaming = false
				m.waiting = false
				m.cancel = nil
				m.streamCh = nil
				m.statusMsg = "Request cancelled."
				m.refreshViewport()
			}
			return m, nil

		case "enter":
			if !m.streaming && !m.waiting {
				return m, m.sendMessage()
			}
			return m, nil

		case "up":
			if !m.streaming && !m.waiting {
				m.navigateHistory(true)
			}
			return m, nil

		case "down":
			if !m.streaming && !m.waiting {
				m.navigateHistory(false)
			}
			return m, nil
		}

	case streamTokenMsg:
		m.waiting = false
		m.streaming = true
		m.streamContent.WriteString(string(msg))
		m.refreshViewport()
		return m, m.pollStream()

	case streamDoneMsg:
		content := m.streamContent.String()
		m.session.AddAssistantMessage(content)
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "AI",
			Content: content,
		})
		m.streaming = false
		m.waiting = false
		m.streamContent.Reset()
		m.cancel = nil
		m.streamCh = nil
		m.statusMsg = ""
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case streamErrMsg:
		if m.streamContent.Len() > 0 {
			partial := m.streamContent.String()
			m.session.AddAssistantMessage(partial)
			m.display = append(m.display, chat.DisplayMessage{
				Role:    "AI",
				Content: partial,
			})
		}
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Error",
			Content: msg.err.Error(),
			IsError: true,
		})
		m.streaming = false
		m.waiting = false
		m.streamContent.Reset()
		m.cancel = nil
		m.streamCh = nil
		m.statusMsg = ""
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case spinner.TickMsg:
		if m.waiting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.refreshViewport()
			return m, cmd
		}
	}

	var cmds []tea.Cmd

	if m.streaming || m.waiting {
		if cmd := m.pollStream(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	headerH := 3
	footerH := 3
	inputH := 5
	vpHeight := m.height - headerH - footerH - inputH
	if vpHeight < 4 {
		vpHeight = 4
	}

	innerWidth := m.width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	if !m.ready {
		m.viewport = viewport.New(innerWidth, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = vpHeight
	}

	m.textarea.SetWidth(innerWidth - 2)
	m.refreshViewport()
	m.viewport.GotoBottom()

	return m, nil
}

func (m Model) pollStream() tea.Cmd {
	if m.streamCh == nil {
		return nil
	}
	ch := m.streamCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		if ev.Err != nil {
			return streamErrMsg{err: ev.Err}
		}
		if ev.Done {
			return streamDoneMsg{}
		}
		if ev.Token != "" {
			return streamTokenMsg(ev.Token)
		}
		return nil
	}
}

func (m *Model) sendMessage() tea.Cmd {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return nil
	}

	if cmdResult := chat.HandleCommand(input, m.session); cmdResult != nil {
		m.textarea.Reset()
		if cmdResult.Quit {
			m.quitting = true
			return tea.Quit
		}
		if cmdResult.NewModel != "" {
			m.client.SetModel(cmdResult.NewModel)
		}
		if cmdResult.Display != nil {
			m.display = append(m.display, *cmdResult.Display)
			m.refreshViewport()
			m.viewport.GotoBottom()
		}
		return nil
	}

	m.pushInputHistory(input)
	m.display = append(m.display, chat.DisplayMessage{
		Role:    "You",
		Content: input,
	})
	m.session.AddUserMessage(input)
	m.textarea.Reset()
	m.historyIndex = len(m.inputHistory)

	m.waiting = true
	m.streaming = false
	m.streamContent.Reset()
	m.statusMsg = ""
	m.refreshViewport()
	m.viewport.GotoBottom()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	ch, err := m.client.Stream(ctx, m.session.Messages())
	if err != nil {
		cancel()
		m.waiting = false
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Error",
			Content: err.Error(),
			IsError: true,
		})
		m.refreshViewport()
		m.viewport.GotoBottom()
		return nil
	}
	m.streamCh = ch

	return tea.Batch(m.spinner.Tick, m.pollStream())
}

func (m *Model) pushInputHistory(input string) {
	if len(m.inputHistory) > 0 && m.inputHistory[len(m.inputHistory)-1] == input {
		return
	}
	m.inputHistory = append(m.inputHistory, input)
}

func (m *Model) navigateHistory(up bool) {
	if len(m.inputHistory) == 0 {
		return
	}
	if up {
		if m.historyIndex > 0 {
			m.historyIndex--
		} else if m.historyIndex == len(m.inputHistory) {
			m.historyIndex = len(m.inputHistory) - 1
		}
	} else {
		if m.historyIndex < len(m.inputHistory)-1 {
			m.historyIndex++
		} else {
			m.historyIndex = len(m.inputHistory)
			m.textarea.SetValue("")
			return
		}
	}
	if m.historyIndex >= 0 && m.historyIndex < len(m.inputHistory) {
		m.textarea.SetValue(m.inputHistory[m.historyIndex])
		m.textarea.CursorEnd()
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	innerWidth := m.width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	header := renderHeader(innerWidth, m.session.Model())
	chatView := m.viewport.View()
	input := renderInput(innerWidth, m.textarea.View())
	footer := renderFooter(m.statusMsg)

	inner := lipgloss.JoinVertical(lipgloss.Left, chatView, input)
	content := lipgloss.JoinVertical(lipgloss.Left, header, inner, footer)

	return appStyle.Width(m.width).Render(content)
}

func (m *Model) refreshViewport() {
	var b strings.Builder

	for _, msg := range m.display {
		b.WriteString(renderMessage(m.viewport.Width, msg))
		b.WriteString("\n")
	}

	if m.waiting {
		b.WriteString(renderMessage(m.viewport.Width, chat.DisplayMessage{
			Role:    "AI",
			Content: m.spinner.View() + " Thinking...",
		}))
		b.WriteString("\n")
	} else if m.streaming {
		content := m.streamContent.String()
		if content == "" {
			content = m.spinner.View()
		}
		b.WriteString(renderMessage(m.viewport.Width, chat.DisplayMessage{
			Role:    "AI",
			Content: content + "▌",
		}))
		b.WriteString("\n")
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func renderMessage(width int, msg chat.DisplayMessage) string {
	var label string
	switch {
	case msg.IsError:
		label = errorLabelStyle.Render(msg.Role)
	case msg.IsInfo:
		label = infoLabelStyle.Render(msg.Role)
	case msg.Role == "You":
		label = userLabelStyle.Render(msg.Role)
	case msg.Role == "AI":
		label = assistantLabelStyle.Render(msg.Role)
	default:
		label = infoLabelStyle.Render(msg.Role)
	}

	bodyWidth := width - 2
	if bodyWidth < 10 {
		bodyWidth = 10
	}
	body := lipgloss.NewStyle().Width(bodyWidth).Render(msg.Content)
	return label + "\n" + body
}

func renderHeader(width int, model string) string {
	title := headerTitleStyle.Render("AI CLI")
	modelText := headerModelStyle.Render(fmt.Sprintf("Model: %s", model))
	gap := width - lipgloss.Width(title) - lipgloss.Width(modelText)
	if gap < 1 {
		gap = 1
	}
	line := title + strings.Repeat(" ", gap) + modelText
	return headerStyle.Width(width).Render(line)
}

func renderInput(width int, input string) string {
	prompt := promptStyle.Render(">")
	return inputBoxStyle.Width(width).Render(prompt + " " + input)
}

func renderFooter(status string) string {
	hints := footerStyle.Render("Ctrl+C quit · Esc cancel · ↑↓ history · Shift+Enter newline")
	if status != "" {
		return hints + "\n" + statusStyle.Render(status)
	}
	return hints
}

// Run starts the TUI application.
func Run(cfg config.Config, client *llm.Client) error {
	p := tea.NewProgram(NewModel(cfg, client), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
