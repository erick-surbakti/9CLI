package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/user/ai-cli/internal/config"
)

// Client communicates with an OpenAI-compatible API (9Router).
type Client struct {
	apiKey  string
	baseURL string
	model   string
	tools   []Tool
	http    *http.Client
}

// NewClient creates a client from configuration.
func NewClient(cfg config.Config) *Client {
	return &Client{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Model returns the currently configured model name.
func (c *Client) Model() string {
	return c.model
}

// SetModel changes the model for subsequent requests.
func (c *Client) SetModel(model string) {
	c.model = model
}

// SetTools enables tool calling for subsequent requests.
func (c *Client) SetTools(tools []Tool) {
	c.tools = tools
}

// Stream sends a chat completion request and returns a channel of stream events.
func (c *Client) Stream(ctx context.Context, messages []Message) (<-chan StreamEvent, error) {
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
		Tools:    c.tools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	ch := make(chan StreamEvent)

	go func() {
		defer close(ch)

		resp, err := c.http.Do(req)
		if err != nil {
			ch <- StreamEvent{Err: mapNetworkError(err, c.baseURL)}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ch <- StreamEvent{Err: parseHTTPError(resp)}
			return
		}

		if err := readSSE(ctx, resp.Body, ch); err != nil {
			ch <- StreamEvent{Err: err}
		}
	}()

	return ch, nil
}

func mapNetworkError(err error, baseURL string) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("request cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("request timed out")
	}
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connectex") ||
		strings.Contains(errStr, "no such host") {
		return fmt.Errorf("cannot reach 9Router at %s — is it running?", baseURL)
	}
	if strings.Contains(errStr, "context canceled") {
		return fmt.Errorf("request cancelled")
	}
	if strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "Client.Timeout") {
		return fmt.Errorf("request timed out")
	}
	return fmt.Errorf("network error: %w", err)
}

func parseHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyStr := strings.TrimSpace(string(body))

	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &apiErr)

	msg := apiErr.Error.Message
	if msg == "" && bodyStr != "" {
		msg = bodyStr
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid API key — check NINEROUTER_API_KEY")
	case http.StatusNotFound:
		if msg != "" {
			return fmt.Errorf("model not found: %s", msg)
		}
		return fmt.Errorf("endpoint or model not found")
	case http.StatusBadRequest:
		if msg != "" {
			return fmt.Errorf("bad request: %s", msg)
		}
		return fmt.Errorf("invalid request")
	case http.StatusTooManyRequests:
		return fmt.Errorf("rate limited — try again later")
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("9Router unavailable (%d)", resp.StatusCode)
	default:
		if msg != "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

// toolCallDelta is an incremental update to a tool call being streamed.
type toolCallDelta struct {
	index int
	id    string
	name  string
	args  string
}

// readSSE parses Server-Sent Events from the response body.
func readSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent) error {
	scanner := newLineScanner(r)
	var totalContent strings.Builder
	accCalls := map[int]*accToolCall{}
	var fixedCalls []ToolCall

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("request cancelled")
		default:
		}

		line, err := scanner.ReadLine()
		if err == io.EOF {
			ch <- StreamEvent{Done: true, ToolCalls: finalizeCalls(accCalls, fixedCalls)}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read stream: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			ch <- StreamEvent{Done: true, ToolCalls: finalizeCalls(accCalls, fixedCalls)}
			return nil
		}

		token, deltas, fixed, parseErr := parseStreamChunk(data)
		if parseErr != nil {
			return fmt.Errorf("malformed response: %w", parseErr)
		}
		if token != "" {
			totalContent.WriteString(token)
			ch <- StreamEvent{Token: token}
		}
		for _, d := range deltas {
			acc := accCalls[d.index]
			if acc == nil {
				acc = &accToolCall{}
				accCalls[d.index] = acc
			}
			if d.id != "" {
				acc.id = d.id
			}
			if d.name != "" {
				acc.name = d.name
			}
			acc.args.WriteString(d.args)
		}
		if len(fixed) > 0 {
			fixedCalls = fixed
		}
	}
}

// accToolCall accumulates partial tool-call fragments from the stream.
type accToolCall struct {
	id   string
	name string
	args strings.Builder
}

func finalizeCalls(acc map[int]*accToolCall, fixed []ToolCall) []ToolCall {
	if len(fixed) > 0 {
		return fixed
	}
	if len(acc) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(acc))
	for i := 0; i < len(acc); i++ {
		a, ok := acc[i]
		if !ok {
			continue
		}
		args := a.args.String()
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		out = append(out, ToolCall{
			ID:   a.id,
			Type: "function",
			Function: ToolCallFunction{
				Name:      a.name,
				Arguments: json.RawMessage(args),
			},
		})
	}
	return out
}

func parseStreamChunk(data string) (string, []toolCallDelta, []ToolCall, error) {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return "", nil, nil, err
	}

	if len(chunk.Choices) == 0 {
		return "", nil, nil, nil
	}

	choice := chunk.Choices[0]
	if choice.Delta.Content != "" {
		return choice.Delta.Content, nil, nil, nil
	}
	if len(choice.Delta.ToolCalls) > 0 {
		var deltas []toolCallDelta
		for _, tc := range choice.Delta.ToolCalls {
			deltas = append(deltas, toolCallDelta{
				index: tc.Index,
				id:    tc.ID,
				name:  tc.Function.Name,
				args:  tc.Function.Arguments,
			})
		}
		return "", deltas, nil, nil
	}
	if len(choice.Message.ToolCalls) > 0 {
		return "", nil, choice.Message.ToolCalls, nil
	}
	if choice.Message.Content != "" {
		return choice.Message.Content, nil, nil, nil
	}
	if choice.Text != "" {
		return choice.Text, nil, nil, nil
	}
	return "", nil, nil, nil
}
