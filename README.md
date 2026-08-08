# AI CLI — Local Terminal Chatbot & Agent for 9Router or Other BYOK Services

A lightweight, local-first **terminal AI assistant** built in Go. It runs a modern
terminal UI (TUI) for chatting with LLMs and acting on your filesystem — with
**9Router** acting purely as the OpenAI-compatible API gateway.

You talk to it in natural language. It can read your files, search your project,
edit code, and run shell commands — **every write and command requires your
explicit approval first**.

---

## Table of Contents 

- [Features](#features)
- [How it was built (tech stack)](#how-it-was-built-tech-stack)
- [How it works (architecture & process)](#how-it-works-architecture--process)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Usage](#usage)
- [Agent tools & approval](#agent-tools--approval)
- [Project structure](#project-structure)
- [Security](#security)
- [Development](#development)
- [Roadmap](#roadmap)

---

## Features

- 💬 **Streaming chat** with token-by-token responses (SSE) — no waiting for the
  whole reply.
- 🧠 **Multi-turn agent loop** — the model can call tools, see results, and keep
  going (capped at 25 turns to prevent runaway loops).
- 📂 **Filesystem tools** — list, read, and search files; write files and run
  shell commands **only after you approve**.
- 🔍 **Slash-command autocomplete** — type `/` and get live suggestions, just
  like opencode; `Tab` completes.
- 🗂 **Interactive model picker** — switch models at runtime (`/model`), grouped
  by provider (deepseek, gemini, opencode, …).
- ⚡ **Input history**, scrollable chat, `/clear`, `/help`, and graceful
  request cancellation with `Esc`.
- 🔐 **Local & private** — your API key never leaves your machine; the only
  network call is to your own 9Router instance.

---

## How it was built (tech stack)

| Layer | Technology | Role |
|-------|-----------|------|
| Language | **Go 1.22+** | Compiled, single-binary, fast startup, cross-platform |
| TUI framework | **Bubble Tea** (`charmbracelet/bubbletea v1.2.4`) | Elm-style model/view/update loop driving the whole UI |
| UI widgets | **Bubbles** (`charmbracelet/bubbles v0.20.0`) | `textarea` (input), `viewport` (scrolling chat), `spinner` (loading) |
| Styling | **Lipgloss** (`charmbracelet/lipgloss v1.0.0`) | Terminal styling: borders, bubbles, colors, layout |
| Backend API | **9Router** | Self-hosted OpenAI-compatible gateway over HTTP/JSON + SSE |
| Concurrency | **Go goroutines + channels** | Streaming parser → TUI, and the agent tool loop |

The whole app compiles to **a single executable** (`ai-cli.exe`) with no runtime
dependencies — that is why it starts in a fraction of a second.

---

## How it works (architecture & process)

### High-level data flow

```
             ┌──────────────────────────────────────────────┐
             │                  Terminal                    │
             └──────────────────────┬───────────────────────┘
                                    │ keys / rendering
                                    ▼
             ┌──────────────────────────────────────────────┐
             │  Bubble Tea TUI (internal/tui)               │
             │   textarea · viewport · spinner · picker     │
             │   approval prompts · autocomplete menu       │
             └──────┬───────────────────────────┬───────────┘
                    │ session history           │ tool calls & results
                    ▼                           ▼
        ┌───────────────────────┐    ┌──────────────────────────┐
        │  Chat session         │    │  Agent (internal/agent)  │
        │  internal/chat        │    │  list_dir · read_file    │
        │  messages + commands  │    │  write_file · run_command│
        └──────────┬────────────┘    │  search_files            │
                   │                 └───────────┬──────────────┘
                   ▼                             │ (on approval)
        ┌────────────────────────────────────────▼──────────────┐
        │  LLM client (internal/llm)  — OpenAI-compatible client │
        │  SSE streaming · tool calling · model selection        │
        └──────────────────────────────┬────────────────────────┘
                                       ▼
                       ┌─────────────────────────────────────┐
                       │  9Router  /v1/chat/completions      │
                       │  (OpenAI-compatible API gateway)    │
                       └─────────────────────────────────────┘
```

### Step-by-step: how a message becomes a reply

1. **Input** — the `textarea` captures your message. `Enter` sends it,
   `Shift+Enter` makes a new line.
2. **Command or chat?** — the input is routed to `internal/chat`. If it starts
   with `/` it's a slash command (`/model`, `/read`, `/run`, …); otherwise it's
   appended to the conversation history.
3. **Request** — the LLM client marshals the full message history (plus the
   available tool schemas) into an OpenAI-compatible `chat/completions` request
   and streams the response over **Server-Sent Events**.
4. **Streaming render** — every token arrives asynchronously through a Go
   channel, is appended to the visible reply bubble, and the viewport scrolls to
   the bottom.
5. **Tool calls** — if the model requests a tool (`read_file`, `run_command`, …),
   the TUI pauses and shows you exactly what it wants to do:
   `Approve? [y/n]`.
   - `y` → the agent executes it, the result is appended as a `tool` message,
     and the model is called again (**multi-turn loop**).
   - `n` → a "user declined" result is sent back and the model must continue
     without it.
6. **Done** — when no more tool calls come back, the final answer is committed
   to the session and the UI returns to idle.

### The multi-turn agent loop

```
model request ──► stream ──► text? ──► show to user ──► commit ──► idle
                     │
                     └── tool_calls? ──► show approval ──► y/n
                                                         │      │
                                                      execute   decline
                                                         │      │
                                                     feed result back
                                                         │
                                                     model request (again)
```

Each round increments a turn counter; at **25 turns** the loop stops and
prompts you to continue or `/clear`.

---

## Requirements

- **Go 1.22 or later** (only needed to build from source)
- A **running 9Router instance** (the local API gateway)
- A **valid 9Router API key**
- Any modern terminal (Windows Terminal, PowerShell, iTerm2, GNOME Terminal…)

---

## Quick start

```bash
# 1. clone / enter the project directory
cd ai-cli

# 2. configure your environment
cp .env.example .env      # then edit .env with your key

# 3. run in development
go run ./cmd

# or build a single binary
go build -o ai-cli ./cmd
./ai-cli
```

On Windows the built binary is `ai-cli.exe`.

---

## Configuration

All configuration comes from environment variables (loaded from `.env` in the
project directory, or your shell).

| Variable | Required | Default | Description |
|----------|:--------:|---------|-------------|
| `NINEROUTER_API_KEY` | **Yes** | — | Your 9Router API key |
| `NINEROUTER_BASE_URL` | No | `http://localhost:20128/v1` | 9Router API base URL |
| `NINEROUTER_MODEL` | No | `gpt-5` | Default model |
| `NINEROUTER_TIMEOUT` | No | `120s` | HTTP request timeout |

**Windows (PowerShell)**

```powershell
$env:NINEROUTER_API_KEY = "your-api-key"
$env:NINEROUTER_BASE_URL = "http://localhost:20128/v1"
```

**Linux / macOS**

```bash
export NINEROUTER_API_KEY="your-api-key"
export NINEROUTER_BASE_URL="http://localhost:20128/v1"
```

---

## Usage

### Slash commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/exit`, `/quit` | Exit the application |
| `/clear` | Clear the current conversation |
| `/model` | Open the interactive model picker |
| `/model <name>` | Switch to a specific available model |
| `/profile` | Open the profile editor (Name, Hobby, Personalization) |
| `/profile clear` | Forget your profile (alias: `/memory`) |
| `/list [path]` | List files in a directory |
| `/read <file>` | Read a file from disk |
| `/search <query>` | Search file contents recursively |
| `/run <command>` | Run a shell command |
| `/write <file>` | Write a file (content on the following lines, `Shift+Enter`) |

### Slash-command autocomplete

Type `/` and a suggestion menu appears. It filters as you type:

- `↑↓` — move the highlight
- `Tab` or `Enter` — complete the highlighted command
- `Esc` — dismiss

```
Slash commands — Tab to complete, ↑↓ to move, Esc to dismiss
  ▸ /model
    /list
    /read
```

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message / confirm |
| `Tab` | Autocomplete slash command |
| `Shift+Enter` | New line in input |
| `Ctrl+C` | Quit |
| `Esc` | Cancel request / close picker |
| `↑` / `↓` | Input history / navigate menus |
| `PgUp` / `PgDn` | Scroll chat history |
| `y` / `n` | Approve / decline a tool call |

### Switching models

Set the default with the environment variable, or switch at runtime:

```
/model
```

The picker lists **only the models returned by your 9Router** `/v1/models`
endpoint, grouped by provider, with your current model marked `●`.

### Memory & profile

The AI can **remember who you are across sessions**. `/profile` opens an
**interactive profile editor** where you fill in structured fields:

```
┌─ Your Profile ─────────────────────────────┐
│  ▸ Name            : Rizky                 │
│    Hobby           : Coding, gaming        │
│    Personalization : Reply in Indonesian   │
│    [ Save & Close ]                        │
└────────────────────────────────────────────┘
```

- `Enter` — edit the selected field (type, then `Enter` to commit, `Esc` to cancel)
- `↑↓` — move between fields / the save row
- `s` or `Enter` on **Save & Close** — save and return to chat
- `Esc` — close without saving
- `/memory set <text>` — quick way to set just the Personalization field
- `/memory clear` / `/profile clear` — erase the profile

The saved profile is injected as a **system message** on every request so the
model knows who it is talking to. It is stored locally in your OS user-config
directory (`%AppData%\ai-cli\memory.json` on Windows) and never leaves your
machine except as part of the chat request to your own 9Router.

---

## Agent tools & approval

The model can call these tools mid-conversation. **Everything it does is shown
to you first; nothing runs silently.**

| Tool | Description | Needs approval |
|------|-------------|----------------|
| `list_dir` | List directory contents | — |
| `read_file` | Read a file's contents (read-only) | — |
| `search_files` | Search file contents recursively (read-only) | — |
| `write_file` | Write/overwrite a file | **Yes** |
| `run_command` | Run a shell command | **Yes** |

Approval prompt example:

```
deepseek wants to run 2 tool call(s):

  1. run_command({"command":"go test ./..."})
  2. write_file({"path":"README.md","content":"..."})

Approve? [y/n]
```

- Reads are allowed anywhere on disk (read-only, low risk).
- Writes and shell commands **always require your `y`** and are never executed
  without it.
- Declined calls are reported back to the model so it adapts its plan.

---

## Project structure

```
├── cmd/
│   └── main.go                Entry point: config → client → TUI
├── internal/
│   ├── config/                Env loading (.env + shell), validation
│   ├── llm/                   OpenAI-compatible client, SSE streaming,
│   │                          tool-call parsing, model list
│   ├── chat/                  Session state, message history, slash commands
│   ├── agent/                 Tool definitions + local execution (files,
│   │                          search, shell)
│   ├── memory/                Persistent user profile (who you are)
│   └── tui/                   Bubble Tea UI: input, viewport, picker,
│                              approval flow, autocomplete
├── go.mod / go.sum            Go module definition
├── .env.example               Configuration template
└── README.md
```

---

## Security

- **API key** is read from environment variables only — never hardcoded, never
  logged or printed.
- `.env` is gitignored — secrets never enter the repository.
- **Every write and shell command requires explicit user approval.** The agent
  cannot silently modify your machine.
- Reads are read-only; search skips `.git`, `node_modules`, and binary files.
- The only external network call is to your **own** 9Router instance.

> Even with approvals, a local agent can still be dangerous if you approve
> blindly. Only approve commands you understand.

---

## Development

```bash
go build ./...        # compile everything
go vet ./...          # static checks
gofmt -w cmd internal # format source
go run ./cmd          # run locally
```

### Adding a new tool

1. Add a spec to `Agent.ToolSpecs()` in `internal/agent/tools.go`.
2. Add a case in `Agent.Execute()`.
3. Set `NeedsApproval` behavior in the TUI (`internal/tui/model.go`).

---

## Roadmap

- `edit_file` (partial, surgical edits instead of full overwrites)
- `git_*` helper tools and `web_fetch`
- Conversation persistence to disk
- Streaming tool-approval with a full diff preview pane
- Session export / copy-to-clipboard

---

*Built with Go + Bubble Tea · powered by 9Router · open source, local-first.*

---
## Feel Free to Improve this code !
---
