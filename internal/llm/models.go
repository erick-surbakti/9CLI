package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ModelInfo describes an available model from 9Router.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

// modelsResponse is the OpenAI-compatible /v1/models payload.
type modelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// ListModels fetches available models from the 9Router /v1/models endpoint.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, mapNetworkError(err, c.baseURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseHTTPError(resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read models: %w", err)
	}

	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("malformed models response: %w", err)
	}

	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("no models available from 9Router")
	}

	models := parsed.Data
	sort.Slice(models, func(i, j int) bool {
		pi, pj := providerName(models[i]), providerName(models[j])
		if pi != pj {
			return pi < pj
		}
		return models[i].ID < models[j].ID
	})

	return models, nil
}

// ProviderName extracts a display group from a model ID (e.g. "deepseek" from "deepseek/chat").
func ProviderName(id string) string {
	return providerName(ModelInfo{ID: id})
}

func providerName(m ModelInfo) string {
	if m.OwnedBy != "" && m.OwnedBy != "9router" {
		return m.OwnedBy
	}
	if idx := strings.Index(m.ID, "/"); idx > 0 {
		return m.ID[:idx]
	}
	if idx := strings.Index(m.ID, "-"); idx > 0 {
		return m.ID[:idx]
	}
	return "other"
}

// FindModel returns the model if it exists in the list (case-sensitive exact match).
func FindModel(models []ModelInfo, id string) (ModelInfo, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// FormatModelList renders a grouped text list of models.
func FormatModelList(models []ModelInfo, current string) string {
	if len(models) == 0 {
		return "No models available."
	}

	var b strings.Builder
	lastProvider := ""

	for _, m := range models {
		p := providerName(m)
		if p != lastProvider {
			if lastProvider != "" {
				b.WriteString("\n")
			}
			b.WriteString(strings.ToUpper(p))
			b.WriteString("\n")
			lastProvider = p
		}
		marker := "  • "
		if m.ID == current {
			marker = "  ▸ "
		}
		b.WriteString(marker)
		b.WriteString(m.ID)
		if m.ID == current {
			b.WriteString(" (active)")
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
