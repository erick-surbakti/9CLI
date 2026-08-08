package main

import (
	"fmt"
	"os"

	"github.com/user/ai-cli/internal/config"
	"github.com/user/ai-cli/internal/llm"
	"github.com/user/ai-cli/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		fmt.Fprintln(os.Stderr, "\nSet NINEROUTER_API_KEY in your environment. See .env.example for details.")
		os.Exit(1)
	}

	client := llm.NewClient(cfg)

	if err := tui.Run(cfg, client); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
