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

// Stream sends a chat completion request and returns a channel of stream events.
func (c *Client) Stream(ctx context.Context, messages []Message) (<-chan StreamEvent, error) {
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
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

// readSSE parses Server-Sent Events from the response body.
func readSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent) error {
	scanner := newLineScanner(r)
	var totalContent strings.Builder

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("request cancelled")
		default:
		}

		line, err := scanner.ReadLine()
		if err == io.EOF {
			if totalContent.Len() == 0 {
				return fmt.Errorf("received empty response from model")
			}
			ch <- StreamEvent{Done: true}
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
			if totalContent.Len() == 0 {
				return fmt.Errorf("received empty response from model")
			}
			ch <- StreamEvent{Done: true}
			return nil
		}

		token, parseErr := parseStreamChunk(data)
		if parseErr != nil {
			return fmt.Errorf("malformed response: %w", parseErr)
		}
		if token != "" {
			totalContent.WriteString(token)
			ch <- StreamEvent{Token: token}
		}
	}
}

func parseStreamChunk(data string) (string, error) {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return "", err
	}

	if len(chunk.Choices) == 0 {
		return "", nil
	}

	choice := chunk.Choices[0]
	if choice.Delta.Content != "" {
		return choice.Delta.Content, nil
	}
	if choice.Message.Content != "" {
		return choice.Message.Content, nil
	}
	if choice.Text != "" {
		return choice.Text, nil
	}
	return "", nil
}
