package chat

import (
	"encoding/json"
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
	memory   string
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
// If a user memory is set, it is injected as a leading system message so the
// model knows who it is talking to.
func (s *Session) Messages() []llm.Message {
	out := make([]llm.Message, 0, len(s.messages)+1)
	if strings.TrimSpace(s.memory) != "" {
		out = append(out, llm.Message{
			Role:    llm.RoleSystem,
			Content: fmt.Sprintf("Remember this about the user and answer accordingly:\n%s", s.memory),
		})
	}
	out = append(out, s.messages...)
	return out
}

// Memory returns the saved user profile for this session.
func (s *Session) Memory() string {
	return s.memory
}

// SetMemory stores the user profile for this session.
func (s *Session) SetMemory(text string) {
	s.memory = strings.TrimSpace(text)
}

// ClearMemory removes the user profile for this session.
func (s *Session) ClearMemory() {
	s.memory = ""
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

// AddAssistantToolCalls appends an assistant message that requests tool calls.
func (s *Session) AddAssistantToolCalls(content string, calls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{
		Role:      llm.RoleAssistant,
		Content:   content,
		ToolCalls: calls,
	})
}

// AddToolResult appends the result of a tool call.
func (s *Session) AddToolResult(id, name, content string) {
	s.messages = append(s.messages, llm.Message{
		Role:       llm.RoleTool,
		Content:    content,
		ToolCallID: id,
		Name:       name,
	})
}

// Clear removes all conversation history.
func (s *Session) Clear() {
	s.messages = nil
}

// CommandResult is the outcome of handling a slash command.
type CommandResult struct {
	Handled           bool
	Quit              bool
	Display           *DisplayMessage
	NewModel          string
	OpenModelPicker   bool
	OpenProfileEditor bool
	ToolCall          *llm.ToolCall
	MemorySet         *string
	MemoryCleared     bool
}

// Commands returns all known slash commands, for autocomplete.
func Commands() []string {
	return []string{
		"/model",
		"/memory",
		"/profile",
		"/list",
		"/read",
		"/search",
		"/run",
		"/write",
		"/help",
		"/clear",
		"/exit",
		"/quit",
	}
}

// SuggestCommands returns commands matching the given prefix.
func SuggestCommands(prefix string) []string {
	p := strings.ToLower(strings.TrimSpace(prefix))
	if p == "" || !strings.HasPrefix(p, "/") {
		return nil
	}
	var out []string
	for _, c := range Commands() {
		if strings.HasPrefix(strings.ToLower(c), p) {
			out = append(out, c)
		}
	}
	return out
}

// HandleCommand processes slash commands. Returns nil if input is not a command.
func HandleCommand(input string, session *Session, available []llm.ModelInfo) *CommandResult {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	first := strings.TrimSpace(lines[0])
	parts := strings.Fields(first)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	arg := strings.TrimSpace(strings.TrimPrefix(first, parts[0]))

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
				Handled:         true,
				OpenModelPicker: true,
			}
		}
		if len(available) > 0 {
			if _, ok := llm.FindModel(available, arg); !ok {
				return &CommandResult{
					Handled: true,
					Display: &DisplayMessage{
						Role: "Error",
						Content: fmt.Sprintf("Model %q is not available on your 9Router.\n\nAvailable models:\n%s\n\nUse /model to open the interactive picker.",
							arg, llm.FormatModelList(available, session.Model())),
						IsError: true,
					},
				}
			}
		}
		session.SetModel(arg)
		return &CommandResult{
			Handled:  true,
			NewModel: arg,
			Display: &DisplayMessage{
				Role:    "System",
				Content: fmt.Sprintf("Model switched to %s", arg),
				IsInfo:  true,
			},
		}
	case "/memory", "/profile":
		return handleMemoryCommand(arg, cmd)
	case "/list":
		return toolCommand("list_dir", arg, "path", "Listed directory", arg)
	case "/read":
		if arg == "" {
			return missingArg(cmd, "<file path>")
		}
		return toolCommand("read_file", arg, "path", "Read file", arg)
	case "/search":
		if arg == "" {
			return missingArg(cmd, "<query>")
		}
		return toolCommand("search_files", arg, "query", "Search results", arg)
	case "/run":
		if arg == "" {
			return missingArg(cmd, "<shell command>")
		}
		return toolCommand("run_command", arg, "command", "Command output", arg)
	case "/write":
		if arg == "" {
			return missingArg(cmd, "<file path>", "content on the following lines (use Shift+Enter)")
		}
		content := strings.Join(lines[1:], "\n")
		return &CommandResult{
			Handled: true,
			ToolCall: &llm.ToolCall{
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "write_file",
					Arguments: mustArgs(map[string]string{"path": arg, "content": content}),
				},
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

func missingArg(cmd string, hints ...string) *CommandResult {
	msg := fmt.Sprintf("Usage: %s %s", cmd, hints[0])
	if len(hints) > 1 {
		msg += "\n" + hints[1]
	}
	return &CommandResult{
		Handled: true,
		Display: &DisplayMessage{Role: "Error", Content: msg, IsError: true},
	}
}

func handleMemoryCommand(arg, cmd string) *CommandResult {
	parts := strings.Fields(arg)
	verb := ""
	if len(parts) > 0 {
		verb = strings.ToLower(parts[0])
	}

	switch verb {
	case "set", "save", "add":
		text := strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
		if text == "" {
			return missingArg(cmd, "set <text>", "or just use /profile to open the profile editor")
		}
		return &CommandResult{
			Handled:   true,
			MemorySet: &text,
			Display: &DisplayMessage{
				Role:    "System",
				Content: fmt.Sprintf("Profile saved. The AI now knows:\n\n%s", text),
				IsInfo:  true,
			},
		}
	case "clear", "reset", "delete":
		return &CommandResult{
			Handled:       true,
			MemoryCleared: true,
			Display: &DisplayMessage{
				Role:    "System",
				Content: "Profile cleared. The AI no longer remembers you.",
				IsInfo:  true,
			},
		}
	case "show", "view", "get":
		return &CommandResult{
			Handled:           true,
			OpenProfileEditor: true,
		}
	default:
		if verb != "" {
			return &CommandResult{
				Handled:   true,
				MemorySet: &arg,
				Display: &DisplayMessage{
					Role:    "System",
					Content: fmt.Sprintf("Profile saved. The AI now knows:\n\n%s", arg),
					IsInfo:  true,
				},
			}
		}
		return &CommandResult{
			Handled:           true,
			OpenProfileEditor: true,
		}
	}
}

func toolCommand(name, arg, field, label, echo string) *CommandResult {
	return &CommandResult{
		Handled: true,
		ToolCall: &llm.ToolCall{
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      name,
				Arguments: mustArgs(map[string]string{field: arg}),
			},
		},
	}
}

func mustArgs(m map[string]string) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

//all comment or feature in here
func helpText() string {
	return strings.TrimSpace(`Available commands:

  /help              Show this help message
  /exit              Exit the application
  /clear             Clear the current conversation
  /model             Open interactive model picker
  /model <name>      Switch to a specific available model
  /profile           Open your profile editor (Name, Hobby, Personalization)
  /profile clear     Forget your profile (also: /memory)
  /list [path]       List files in a directory
  /read <file>       Read a file from disk
  /search <query>    Search file contents
  /run <command>     Run a shell command
  /write <file>      Write a file (content on following lines)

Keyboard shortcuts:

  Enter              Send message / select model
  Tab                Autocomplete slash command
  Ctrl+C             Quit
  Esc                Cancel request or close picker
  Up/Down            History or navigate model list
  PgUp/PgDn          Scroll chat view
  Shift+Enter        New line in input`)
}
