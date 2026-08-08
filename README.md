# AI CLI

A local terminal AI chatbot that connects to your 9Router instance. The CLI provides a modern TUI for chatting with LLMs — 9Router acts only as the OpenAI-compatible API gateway.

## Architecture

```
Terminal
  ↓
Bubble Tea TUI          (rendering, input, scroll)
  ↓
Go application
  ↓
Chat session            (history, slash commands)
  ↓
LLM client              (streaming, model selection)
  ↓
9Router OpenAI API      (http://localhost:20128/v1)
  ↓
LLM provider
```

## Requirements

- Go 1.22 or later
- A running 9Router instance
- A valid 9Router API key

## Configuration

Copy the example environment file and fill in your values:

```bash
cp .env.example .env
```

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `NINEROUTER_API_KEY` | Yes | — | API key for 9Router |
| `NINEROUTER_BASE_URL` | No | `http://localhost:20128/v1` | 9Router API base URL |
| `NINEROUTER_MODEL` | No | `gpt-5` | Default model name |
| `NINEROUTER_TIMEOUT` | No | `120s` | HTTP request timeout |

### Windows (PowerShell)

```powershell
$env:NINEROUTER_API_KEY = "your-api-key"
$env:NINEROUTER_BASE_URL = "http://localhost:20128/v1"
$env:NINEROUTER_MODEL = "gpt-5"
```

### Linux / macOS

```bash
export NINEROUTER_API_KEY="your-api-key"
export NINEROUTER_BASE_URL="http://localhost:20128/v1"
export NINEROUTER_MODEL="gpt-5"
```

## Run

Development:

```bash
go run ./cmd
```

Build:

```bash
go build -o ai-cli ./cmd
./ai-cli
```

## Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/exit` | Exit the application |
| `/clear` | Clear the current conversation |
| `/model` | Show the current model |
| `/model <name>` | Change the model for this session |

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| Enter | Send message |
| Shift+Enter | New line in input |
| Ctrl+C | Quit |
| Esc | Cancel current request |
| Up / Down | Navigate input history |

## Changing models

Set the default model via environment variable:

```bash
export NINEROUTER_MODEL="claude-sonnet-4"
```

Or change it during a session:

```
/model claude-sonnet-4
```

## Project structure

```
├── cmd/main.go              Entry point
├── internal/
│   ├── config/              Environment configuration
│   ├── llm/                 OpenAI-compatible API client + streaming
│   ├── chat/                Session history + slash commands
│   └── tui/                 Bubble Tea interface
├── go.mod
├── .env.example
└── README.md
```

## Security

- API keys are loaded from environment variables only
- Keys are never logged or printed
- `.env` is gitignored — never commit secrets

## Future extensions

The architecture is designed so agent and tool capabilities can be added later:

- `llm.Message` includes a reserved `tool` role
- LLM client is isolated from the TUI
- Slash commands can be extended in `internal/chat`
