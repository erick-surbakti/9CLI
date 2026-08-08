package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/ai-cli/internal/agent"
	"github.com/user/ai-cli/internal/chat"
	"github.com/user/ai-cli/internal/config"
	"github.com/user/ai-cli/internal/llm"
	"github.com/user/ai-cli/internal/memory"
)

type streamTokenMsg string

type streamDoneMsg struct {
	toolCalls []llm.ToolCall
}

type streamErrMsg struct{ err error }

type modelsLoadedMsg struct {
	models []llm.ModelInfo
}

type modelsErrMsg struct{ err error }

type tickThinkingMsg struct{}

const (
	stateBooting = iota
	stateReady
)

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

	appState        int
	bootMessage     string
	availableModels []llm.ModelInfo

	modelPickerOpen bool
	pickerCursor    int
	pickerOffset    int

	streaming     bool
	waiting       bool
	streamContent strings.Builder
	cancel        context.CancelFunc
	streamCh      <-chan llm.StreamEvent
	thinkingFrame int

	inputHistory []string
	historyIndex int

	statusMsg string
	quitting  bool

	agent           *agent.Agent
	pendingCalls    []llm.ToolCall
	approvalPending bool
	turnCount       int

	suggestOpen  bool
	suggestions  []string
	suggestIndex int

	profile           memory.Profile
	profileEditorOpen bool
	profileIndex      int
	profileEditing    bool
}

const maxTurns = 25

// profileRows is the number of selectable rows in the profile editor
// (fields + the Save & Close row).
var profileRows = len(profileFields) + 1

var profileFields = []struct {
	Key   string
	Label string
}{
	{"name", "Name"},
	{"hobby", "Hobby"},
	{"personalization", "Personalization"},
}

// NewModel creates the TUI model.
func NewModel(cfg config.Config, client *llm.Client) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message or /model to switch..."
	ta.Focus()
	ta.CharLimit = 8000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"))

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = splashSpinnerStyle

	wd, _ := os.Getwd()
	agt := agent.New(wd)
	client.SetTools(agt.ToolSpecs())

	session := chat.NewSession(cfg.Model)
	var prof memory.Profile
	if loaded, err := memory.Load(); err == nil {
		prof = loaded
	}
	session.SetMemory(prof.Prompt())

	return Model{
		cfg:         cfg,
		client:      client,
		session:     session,
		textarea:    ta,
		spinner:     sp,
		agent:       agt,
		profile:     prof,
		appState:    stateBooting,
		bootMessage: "Connecting to 9Router",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
		m.fetchModels(),
		tea.Tick(time.Millisecond*400, func(t time.Time) tea.Msg { return tickThinkingMsg{} }),
	)
}

func (m Model) fetchModels() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		models, err := m.client.ListModels(ctx)
		if err != nil {
			return modelsErrMsg{err: err}
		}
		return modelsLoadedMsg{models: models}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case modelsLoadedMsg:
		m.availableModels = msg.models
		m.appState = stateReady
		m.syncDefaultModel()
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "System",
			Content: welcomeText(m.session.Model(), len(m.availableModels)),
			IsInfo:  true,
		})
		if mem := m.session.Memory(); mem != "" {
			m.display = append(m.display, chat.DisplayMessage{
				Role:    "System",
				Content: "Profile loaded — the AI remembers you. Type /memory to view or /memory clear to forget.",
				IsInfo:  true,
			})
		}
		m.refreshViewport()
		return m, nil

	case modelsErrMsg:
		m.appState = stateReady
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Error",
			Content: fmt.Sprintf("Could not load models from 9Router: %s\n\nUsing configured model %q. Retry with /model after 9Router is available.", msg.err, m.session.Model()),
			IsError: true,
		})
		m.refreshViewport()
		return m, nil

	case tickThinkingMsg:
		if m.waiting {
			m.thinkingFrame = (m.thinkingFrame + 1) % 4
			m.refreshViewport()
		}
		return m, tea.Tick(time.Millisecond*400, func(t time.Time) tea.Msg { return tickThinkingMsg{} })

	case tea.KeyMsg:
		if m.appState == stateBooting {
			return m, nil
		}
		if m.modelPickerOpen {
			return m.handlePickerKeys(msg)
		}
		if m.profileEditorOpen {
			return m.handleProfileKeys(msg)
		}
		return m.handleChatKeys(msg)

	case streamTokenMsg:
		m.waiting = false
		m.streaming = true
		m.streamContent.WriteString(string(msg))
		m.refreshViewport()
		return m, m.pollStream()

	case streamDoneMsg:
		content := m.streamContent.String()
		m.streaming = false
		m.waiting = false
		m.streamContent.Reset()
		m.cancel = nil
		m.streamCh = nil
		m.statusMsg = ""
		m.viewport.GotoBottom()
		if len(msg.toolCalls) > 0 {
			m.session.AddAssistantToolCalls(content, msg.toolCalls)
			m.pendingCalls = msg.toolCalls
			m.approvalPending = true
			if content != "" {
				m.display = append(m.display, chat.DisplayMessage{
					Role:    "AI",
					Content: content,
				})
			}
			m.display = append(m.display, chat.DisplayMessage{
				Role:    "System",
				Content: fmt.Sprintf("%s wants to run %d tool call(s):\n\n%s\n\nApprove? [y/n]", m.session.Model(), len(msg.toolCalls), describeCalls(msg.toolCalls)),
				IsInfo:  true,
			})
			m.refreshViewport()
			m.viewport.GotoBottom()
			return m, nil
		}

		if content != "" {
			m.session.AddAssistantMessage(content)
			m.display = append(m.display, chat.DisplayMessage{
				Role:    "AI",
				Content: content,
			})
		}
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
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.appState == stateBooting || m.waiting || (m.streaming && m.streamContent.Len() == 0) {
			return m, cmd
		}
	}

	var cmds []tea.Cmd

	if !m.modelPickerOpen && m.appState == stateReady {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) syncDefaultModel() {
	if len(m.availableModels) == 0 {
		return
	}
	if _, ok := llm.FindModel(m.availableModels, m.session.Model()); ok {
		return
	}
	first := m.availableModels[0].ID
	m.session.SetModel(first)
	m.client.SetModel(first)
}

func (m Model) handleChatKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.approvalPending {
		switch msg.String() {
		case "y", "Y", "enter", "space":
			return m, m.approvePending()
		case "n", "N", "esc", "ctrl+c":
			return m, m.rejectPending()
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		return m, tea.Quit

	case "esc":
		if m.suggestOpen {
			m.suggestOpen = false
			m.suggestions = nil
			return m, nil
		}
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

	case "tab":
		if m.suggestOpen && len(m.suggestions) > 0 {
			m.completeSuggestion()
		}
		return m, nil

	case "enter":
		if m.suggestOpen && len(m.suggestions) > 0 && m.textarea.Value() != m.suggestions[m.suggestIndex] {
			m.completeSuggestion()
			return m, nil
		}
		m.suggestOpen = false
		m.suggestions = nil
		if !m.streaming && !m.waiting {
			return m, m.sendMessage()
		}
		return m, nil

	case "up":
		if m.suggestOpen && len(m.suggestions) > 0 {
			if m.suggestIndex > 0 {
				m.suggestIndex--
			}
			return m, nil
		}
		if !m.streaming && !m.waiting {
			m.navigateHistory(true)
		}
		return m, nil

	case "down":
		if m.suggestOpen && len(m.suggestions) > 0 {
			if m.suggestIndex < len(m.suggestions)-1 {
				m.suggestIndex++
			}
			return m, nil
		}
		if !m.streaming && !m.waiting {
			m.navigateHistory(false)
		}
		return m, nil

	case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
		if !m.streaming && !m.waiting {
			m.viewport, _ = m.viewport.Update(msg)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.updateSuggestions()
	return m, cmd
}

func (m *Model) updateSuggestions() {
	value := m.textarea.Value()
	sug := chat.SuggestCommands(value)
	if len(sug) == 0 {
		m.suggestOpen = false
		m.suggestions = nil
		m.suggestIndex = 0
		return
	}
	m.suggestions = sug
	m.suggestOpen = true
	if m.suggestIndex >= len(sug) {
		m.suggestIndex = len(sug) - 1
	}
}

func (m *Model) completeSuggestion() {
	if len(m.suggestions) == 0 {
		return
	}
	idx := m.suggestIndex
	if idx >= len(m.suggestions) {
		idx = 0
	}
	m.textarea.SetValue(m.suggestions[idx])
	m.textarea.CursorEnd()
	m.suggestOpen = false
	m.suggestions = nil
	m.suggestIndex = 0
}

func (m Model) handlePickerKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "esc":
		m.modelPickerOpen = false
		m.statusMsg = ""
		return m, nil

	case "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m.ensurePickerVisible()
		}
		return m, nil

	case "down":
		if m.pickerCursor < len(m.availableModels)-1 {
			m.pickerCursor++
			m.ensurePickerVisible()
		}
		return m, nil

	case "enter":
		return m, m.selectPickerModel()
	}
	return m, nil
}

func (m *Model) ensurePickerVisible() {
	visible := 8
	if m.pickerCursor < m.pickerOffset {
		m.pickerOffset = m.pickerCursor
	}
	if m.pickerCursor >= m.pickerOffset+visible {
		m.pickerOffset = m.pickerCursor - visible + 1
	}
}

func (m *Model) openModelPicker() {
	if len(m.availableModels) == 0 {
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Error",
			Content: "No models loaded. Make sure 9Router is running and try again.",
			IsError: true,
		})
		m.refreshViewport()
		return
	}

	m.modelPickerOpen = true
	m.pickerCursor = 0
	m.pickerOffset = 0

	for i, model := range m.availableModels {
		if model.ID == m.session.Model() {
			m.pickerCursor = i
			m.ensurePickerVisible()
			break
		}
	}
}

func (m *Model) selectPickerModel() tea.Cmd {
	if len(m.availableModels) == 0 || m.pickerCursor >= len(m.availableModels) {
		m.modelPickerOpen = false
		return nil
	}

	selected := m.availableModels[m.pickerCursor].ID
	m.session.SetModel(selected)
	m.client.SetModel(selected)
	m.modelPickerOpen = false

	m.display = append(m.display, chat.DisplayMessage{
		Role:    "System",
		Content: fmt.Sprintf("Model switched to %s", selected),
		IsInfo:  true,
	})
	m.refreshViewport()
	m.viewport.GotoBottom()
	return nil
}

// openProfileEditor opens the interactive profile page.
func (m *Model) openProfileEditor() {
	m.profileEditorOpen = true
	m.profileIndex = 0
	m.profileEditing = false
	m.textarea.Reset()
}

// handleProfileKeys processes keys while the profile editor is open.
func (m Model) handleProfileKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.profileEditing {
		switch msg.String() {
		case "enter":
			return m, m.commitProfileField()
		case "esc":
			m.profileEditing = false
			m.textarea.Reset()
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "esc":
		m.profileEditorOpen = false
		return m, nil

	case "up":
		if m.profileIndex > 0 {
			m.profileIndex--
		}
		return m, nil

	case "down":
		if m.profileIndex < profileRows-1 {
			m.profileIndex++
		}
		return m, nil

	case "enter":
		if m.profileIndex == profileRows-1 {
			return m, m.saveProfile()
		}
		return m, m.beginProfileEdit()

	case "s", "ctrl+s":
		return m, m.saveProfile()
	}
	return m, nil
}

// beginProfileEdit loads the selected field into the input for editing.
func (m *Model) beginProfileEdit() tea.Cmd {
	field := profileFields[m.profileIndex]
	m.profileEditing = true
	m.textarea.SetValue(m.profile.Get(field.Key))
	m.textarea.CursorEnd()
	return textarea.Blink
}

// commitProfileField writes the edited value back into the profile.
func (m *Model) commitProfileField() tea.Cmd {
	field := profileFields[m.profileIndex]
	m.profile.Set(field.Key, m.textarea.Value())
	m.profileEditing = false
	m.textarea.Reset()
	return nil
}

// saveProfile persists the profile, updates the session memory, and closes the page.
func (m *Model) saveProfile() tea.Cmd {
	_ = memory.Save(m.profile)
	m.session.SetMemory(m.profile.Prompt())
	m.profileEditorOpen = false
	m.profileEditing = false
	m.textarea.Reset()
	m.display = append(m.display, chat.DisplayMessage{
		Role:    "System",
		Content: "Profile saved. The AI now knows who you are.",
		IsInfo:  true,
	})
	m.refreshViewport()
	m.viewport.GotoBottom()
	return nil
}

// clearProfile wipes the profile and persists the empty state.
func (m *Model) clearProfile() {
	m.profile = memory.Profile{}
	m.session.ClearMemory()
	_ = memory.Save(m.profile)
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
	if m.appState == stateReady {
		m.refreshViewport()
		m.viewport.GotoBottom()
	}

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
			return streamDoneMsg{toolCalls: ev.ToolCalls}
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

	if cmdResult := chat.HandleCommand(input, m.session, m.availableModels); cmdResult != nil {
		m.textarea.Reset()
		m.suggestOpen = false
		m.suggestions = nil
		if cmdResult.Quit {
			m.quitting = true
			return tea.Quit
		}
		if cmdResult.OpenModelPicker {
			m.openModelPicker()
			return nil
		}
		if cmdResult.OpenProfileEditor {
			m.openProfileEditor()
			return nil
		}
		if cmdResult.NewModel != "" {
			m.client.SetModel(cmdResult.NewModel)
		}
		if cmdResult.MemorySet != nil {
			m.profile.Set("personalization", *cmdResult.MemorySet)
			_ = memory.Save(m.profile)
			m.session.SetMemory(m.profile.Prompt())
		}
		if cmdResult.MemoryCleared {
			m.clearProfile()
		}
		if cmdResult.ToolCall != nil {
			content, isErr := m.agent.Execute(*cmdResult.ToolCall)
			m.display = append(m.display, chat.DisplayMessage{
				Role:    "Tool",
				Content: fmt.Sprintf("⚙ %s\n%s", cmdResult.ToolCall.Function.Name, content),
				IsError: isErr,
			})
			m.refreshViewport()
			m.viewport.GotoBottom()
			return nil
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
	m.turnCount = 0

	return m.startTurn()
}

// startTurn begins (or continues) a model request for the current session.
func (m *Model) startTurn() tea.Cmd {
	m.waiting = true
	m.streaming = false
	m.streamContent.Reset()
	m.statusMsg = ""
	m.thinkingFrame = 0
	m.turnCount++
	m.refreshViewport()
	m.viewport.GotoBottom()

	if m.turnCount > maxTurns {
		m.waiting = false
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Error",
			Content: fmt.Sprintf("Stopped after %d turns — the model kept calling tools. Continue with /run or /clear.", maxTurns),
			IsError: true,
		})
		m.refreshViewport()
		m.viewport.GotoBottom()
		return nil
	}

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

func (m *Model) approvePending() tea.Cmd {
	m.approvalPending = false
	calls := m.pendingCalls
	m.pendingCalls = nil

	for _, call := range calls {
		content, isErr := m.agent.Execute(call)
		m.session.AddToolResult(call.ID, call.Function.Name, content)
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Tool",
			Content: fmt.Sprintf("⚙ %s → %s", call.Function.Name, truncateForDisplay(content)),
			IsError: isErr,
		})
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
	return m.startTurn()
}

func (m *Model) rejectPending() tea.Cmd {
	m.approvalPending = false
	calls := m.pendingCalls
	m.pendingCalls = nil

	for _, call := range calls {
		m.session.AddToolResult(call.ID, call.Function.Name, "User declined this tool call. Do not run it again.")
		m.display = append(m.display, chat.DisplayMessage{
			Role:    "Tool",
			Content: fmt.Sprintf("✕ Declined: %s", call.Function.Name),
			IsError: true,
		})
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
	return m.startTurn()
}

func describeCalls(calls []llm.ToolCall) string {
	var b strings.Builder
	for i, c := range calls {
		args := strings.TrimSpace(string(c.Function.Arguments))
		if len(args) > 300 {
			args = args[:300] + "..."
		}
		fmt.Fprintf(&b, "  %d. %s(%s)", i+1, c.Function.Name, args)
		if i < len(calls)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func truncateForDisplay(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 12 {
		lines = lines[:12]
		lines = append(lines, "...")
	}
	out := strings.Join(lines, "\n")
	if len(out) > 800 {
		out = out[:800] + "..."
	}
	return out
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
		return splashView(m.width, m.height, m.spinner.View(), m.bootMessage)
	}

	if m.appState == stateBooting {
		return splashView(m.width, m.height, m.spinner.View(), m.bootMessage)
	}

	innerWidth := m.width - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	header := renderHeader(innerWidth, m.session.Model(), len(m.availableModels))
	chatView := m.viewport.View()

	footer := renderFooter(m.statusMsg, m.modelPickerOpen, m.approvalPending)

	if m.modelPickerOpen {
		picker := renderModelPicker(innerWidth, m.availableModels, m.pickerCursor, m.pickerOffset, m.session.Model())
		content := lipgloss.JoinVertical(lipgloss.Left, header, picker, footer)
		return appStyle.Width(m.width).Render(content)
	}

	if m.profileEditorOpen {
		editor := renderProfileEditor(innerWidth, m.profile, m.profileIndex, m.profileEditing)
		var input string
		if m.profileEditing {
			field := profileFields[m.profileIndex]
			input = renderEditPrompt(innerWidth, field.Label, m.textarea.View())
		} else {
			input = renderProfileHint(innerWidth)
		}
		content := lipgloss.JoinVertical(lipgloss.Left, header, editor, input, footer)
		return appStyle.Width(m.width).Render(content)
	}

	var input string
	if m.modelPickerOpen {
		input = renderPickerHint(innerWidth)
	} else {
		input = renderInput(innerWidth, m.textarea.View())
	}

	var middle []string
	middle = append(middle, chatView)
	if m.suggestOpen && len(m.suggestions) > 0 {
		middle = append(middle, renderSuggestMenu(innerWidth, m.suggestions, m.suggestIndex))
	}
	middle = append(middle, input)

	inner := lipgloss.JoinVertical(lipgloss.Left, middle...)
	content := lipgloss.JoinVertical(lipgloss.Left, header, inner, footer)
	return appStyle.Width(m.width).Render(content)
}

func (m *Model) refreshViewport() {
	var b strings.Builder

	for _, msg := range m.display {
		b.WriteString(renderMessage(m.viewport.Width, msg))
	}

	if m.waiting {
		dots := strings.Repeat(".", m.thinkingFrame)
		if m.thinkingFrame == 0 {
			dots = ""
		}
		b.WriteString(renderMessage(m.viewport.Width, chat.DisplayMessage{
			Role:    "AI",
			Content: m.spinner.View() + " Thinking" + dots,
		}))
	} else if m.streaming {
		content := m.streamContent.String()
		if content == "" {
			content = m.spinner.View() + " "
		}
		b.WriteString(renderMessage(m.viewport.Width, chat.DisplayMessage{
			Role:    "AI",
			Content: content + "▌",
		}))
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func welcomeText(model string, count int) string {
	return fmt.Sprintf(`Welcome! Connected to 9Router with %d model(s) available.

Current model: %s
Type /model to switch models, or /help for commands.`, count, model)
}

func splashView(width, height int, spin, message string) string {
	logo := splashTitleStyle.Render("◆ AI CLI")
	sub := splashSubtitleStyle.Render("Powered by 9Router")
	status := splashSpinnerStyle.Render(spin + " " + message)
	body := lipgloss.JoinVertical(lipgloss.Center, logo, sub, "", status)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(2, 4).
		Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderMessage(width int, msg chat.DisplayMessage) string {
	bodyWidth := width - 4
	if bodyWidth < 10 {
		bodyWidth = 10
	}

	var label string
	var bubble lipgloss.Style
	switch {
	case msg.IsError:
		label = errorLabelStyle.Render("✕ " + msg.Role)
		bubble = errorBubbleStyle
	case msg.IsInfo:
		label = infoLabelStyle.Render("◈ " + msg.Role)
		bubble = infoBubbleStyle
	case msg.Role == "You":
		label = userLabelStyle.Render("You")
		bubble = userBubbleStyle
	case msg.Role == "Tool":
		label = infoLabelStyle.Render("⚙ " + msg.Role)
		bubble = infoBubbleStyle
	default:
		label = assistantLabelStyle.Render("AI")
		bubble = assistantBubbleStyle
	}

	body := lipgloss.NewStyle().Width(bodyWidth).Render(msg.Content)
	return bubble.Width(width).Render(label+"\n"+body) + "\n"
}

func renderHeader(width int, model string, modelCount int) string {
	title := headerTitleStyle.Render("◆ AI CLI")
	badge := headerBadgeStyle.Render(fmt.Sprintf("%d models", modelCount))
	modelText := headerModelStyle.Render(model)

	titlePart := title + "  " + badge
	gap := width - lipgloss.Width(titlePart) - lipgloss.Width(modelText)
	if gap < 1 {
		gap = 1
	}
	line := titlePart + strings.Repeat(" ", gap) + modelText
	return headerStyle.Width(width).Render(line)
}

func renderInput(width int, input string) string {
	prompt := promptStyle.Render("❯")
	return inputBoxStyle.Width(width).Render(prompt + " " + input)
}

func renderPickerHint(width int) string {
	return inputBoxStyle.Width(width).Render(pickerHintStyle.Render("Model picker open — use ↑↓ and Enter, Esc to close"))
}

func renderFooter(status string, pickerOpen, approval bool) string {
	var hints string
	switch {
	case approval:
		hints = footerStyle.Render("Approve tool call? y yes · n no · Esc no")
	case pickerOpen:
		hints = footerStyle.Render("↑↓ navigate · Enter select · Esc close")
	default:
		hints = footerStyle.Render("Enter send · /model switch · Tab autocomplete · Esc cancel · Ctrl+C quit · ↑↓ history · PgUp/PgDn scroll")
	}
	if status != "" {
		return hints + "\n" + statusStyle.Render(status)
	}
	return hints
}

func renderSuggestMenu(width int, items []string, index int) string {
	var lines []string
	lines = append(lines, suggestHintStyle.Render("Slash commands — Tab to complete, ↑↓ to move, Esc to dismiss"))
	for i, it := range items {
		if i == index {
			lines = append(lines, pickerActiveStyle.Render("▸ "+it))
		} else {
			lines = append(lines, pickerItemStyle.Render("  "+it))
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return suggestBoxStyle.Width(width).Render(body)
}

func renderProfileEditor(width int, p memory.Profile, index int, editing bool) string {
	title := pickerTitleStyle.Render("Your Profile")
	subtitle := pickerHintStyle.Render("Enter to edit a field · ↑↓ to move · s to save · Esc to close")

	valueWidth := width - 30
	if valueWidth < 10 {
		valueWidth = 10
	}

	var lines []string
	lines = append(lines, title, subtitle, "")

	for i, f := range profileFields {
		value := p.Get(f.Key)
		if len(value) > valueWidth {
			value = value[:valueWidth-1] + "…"
		}
		row := fmt.Sprintf("%-16s : %s", f.Label, value)
		if i == index {
			lines = append(lines, pickerActiveStyle.Render("▸ "+row))
		} else {
			lines = append(lines, pickerItemStyle.Render("  "+row))
		}
	}

	saveRow := len(profileFields)
	row := "[ Save & Close ]"
	if saveRow == index {
		lines = append(lines, pickerActiveStyle.Render("▸ "+row))
	} else {
		lines = append(lines, pickerItemStyle.Render("  "+row))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return pickerBoxStyle.Width(min(width, 72)).Render(body)
}

func renderEditPrompt(width int, label, input string) string {
	prompt := promptStyle.Render("Edit " + label + ":")
	return inputBoxStyle.Width(width).Render(prompt + " " + input)
}

func renderProfileHint(width int) string {
	return inputBoxStyle.Width(width).Render(pickerHintStyle.Render("Profile editor — ↑↓ select · Enter edit · s save · Esc close"))
}

func renderModelPicker(width int, models []llm.ModelInfo, cursor, offset int, current string) string {
	const visible = 10

	title := pickerTitleStyle.Render("Select Model")
	subtitle := pickerHintStyle.Render("Only models available on your 9Router are shown")

	var lines []string
	lastProvider := ""

	end := offset + visible
	if end > len(models) {
		end = len(models)
	}

	for i := offset; i < end; i++ {
		m := models[i]
		p := llm.ProviderName(m.ID)
		if p != lastProvider {
			lines = append(lines, pickerProviderStyle.Render(strings.ToUpper(p)))
			lastProvider = p
		}

		if i == cursor {
			lines = append(lines, pickerActiveStyle.Render("▸ "+m.ID))
		} else {
			label := "  " + m.ID
			if m.ID == current {
				label += " ●"
			}
			lines = append(lines, pickerItemStyle.Render(label))
		}
	}

	if len(models) > visible {
		lines = append(lines, pickerHintStyle.Render(fmt.Sprintf("Showing %d–%d of %d", offset+1, end, len(models))))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, append([]string{title, subtitle, ""}, lines...)...)
	return pickerBoxStyle.Width(min(width, 60)).Render(body)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Run starts the TUI application.
func Run(cfg config.Config, client *llm.Client) error {
	p := tea.NewProgram(NewModel(cfg, client), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
