package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://localhost:20128/v1"
	defaultModel   = "gpt-5"
	defaultTimeout = 120 * time.Second
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

// Load reads configuration from environment variables and validates required fields.
func Load() (Config, error) {
	cfg := Config{
		APIKey:  strings.TrimSpace(os.Getenv("NINEROUTER_API_KEY")),
		BaseURL: strings.TrimSpace(os.Getenv("NINEROUTER_BASE_URL")),
		Model:   strings.TrimSpace(os.Getenv("NINEROUTER_MODEL")),
		Timeout: defaultTimeout,
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if cfg.Model == "" {
		cfg.Model = defaultModel
	}

	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("NINEROUTER_API_KEY is required (set it in your environment or .env file)")
	}

	if timeoutStr := strings.TrimSpace(os.Getenv("NINEROUTER_TIMEOUT")); timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid NINEROUTER_TIMEOUT %q: %w", timeoutStr, err)
		}
		cfg.Timeout = d
	}

	return cfg, nil
}
