package llm

// Role represents a message role in the conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool" // reserved for future agent/tool support
)

// Message is a single chat message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is sent to the OpenAI-compatible completions endpoint.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// StreamEvent represents one event from a streaming response.
type StreamEvent struct {
	Token string
	Done  bool
	Err   error
}
