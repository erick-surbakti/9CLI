package chat

import (
	"fmt"
	"strings"

	"github.com/user/ai-cli/internal/llm"
)

// DisplayMessage is a message shown in the TUI (includes system notices).
type DisplayMessage struct {
	Role    string
	Content string
	IsError bool
	IsInfo  bool
}

// Session manages conversation state for the current CLI session.
type Session struct {
	messages []llm.Message
	model    string
}

// NewSession creates a session with the given default model.
func NewSession(model string) *Session {
	return &Session{
		model: model,
	}
}

// Model returns the current session model.
func (s *Session) Model() string {
	return s.model
}

// SetModel changes the model for the current session.
func (s *Session) SetModel(model string) {
	s.model = strings.TrimSpace(model)
}

// Messages returns a copy of the LLM conversation history.
func (s *Session) Messages() []llm.Message {
	out := make([]llm.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// AddUserMessage appends a user message to history.
func (s *Session) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{
		Role:    llm.RoleUser,
		Content: content,
	})
}

// AddAssistantMessage appends an assistant message to history.
func (s *Session) AddAssistantMessage(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.messages = append(s.messages, llm.Message{
		Role:    llm.RoleAssistant,
		Content: content,
	})
}

// Clear removes all conversation history.
func (s *Session) Clear() {
	s.messages = nil
}

// CommandResult is the outcome of handling a slash command.
type CommandResult struct {
	Handled   bool
	Quit      bool
	Display   *DisplayMessage
	NewModel  string
	ModelOnly bool
}

// HandleCommand processes slash commands. Returns nil if input is not a command.
func HandleCommand(input string, session *Session) *CommandResult {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	arg := strings.TrimSpace(strings.TrimPrefix(input, parts[0]))

	switch cmd {
	case "/help":
		return &CommandResult{
			Handled: true,
			Display: &DisplayMessage{
				Role:    "System",
				Content: helpText(),
				IsInfo:  true,
			},
		}
	case "/exit", "/quit":
		return &CommandResult{Handled: true, Quit: true}
	case "/clear":
		session.Clear()
		return &CommandResult{
			Handled: true,
			Display: &DisplayMessage{
				Role:    "System",
				Content: "Conversation cleared.",
				IsInfo:  true,
			},
		}
	case "/model":
		if arg == "" {
			return &CommandResult{
				Handled:   true,
				ModelOnly: true,
				Display: &DisplayMessage{
					Role:    "System",
					Content: fmt.Sprintf("Current model: %s", session.Model()),
					IsInfo:  true,
				},
			}
		}
		session.SetModel(arg)
		return &CommandResult{
			Handled:  true,
			NewModel: arg,
			Display: &DisplayMessage{
				Role:    "System",
				Content: fmt.Sprintf("Model changed to: %s", arg),
				IsInfo:  true,
			},
		}
	default:
		return &CommandResult{
			Handled: true,
			Display: &DisplayMessage{
				Role:    "System",
				Content: fmt.Sprintf("Unknown command: %s\nType /help for available commands.", cmd),
				IsError: true,
			},
		}
	}
}

func helpText() string {
	return strings.TrimSpace(`Available commands:

  /help              Show this help message
  /exit              Exit the application
  /clear             Clear the current conversation
  /model             Show the current model
  /model <name>      Change the model for this session

Keyboard shortcuts:

  Enter              Send message
  Ctrl+C             Quit
  Esc                Cancel current request
  Up/Down            Navigate input history`)
}
