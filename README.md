# gollm — local-first AI coding agent

**Primitives, not features. Local-first. Extensible.**

`gollm` is a powerful, local-first AI agentic harness designed for developers who want a flexible and reliable assistant that runs on their own hardware. It prioritizes local LLMs (via Ollama and llama.cpp) but adapts seamlessly to cloud providers like OpenAI, Anthropic, and Google Gemini.

> A Golem is designed to be a tireless servant to its creator. Brought to life through ritual, created entirely from inanimate matter. It performs physical labor or provides protection.

---

## Core Philosophy

- **Local-First** — Built from the ground up to favor local inference for privacy, speed, and cost-efficiency.
- **Aggressively Extensible** — Every tool, provider, and behavior is a plugin interface. Supports gRPC extensions, markdown skills, and reusable prompt templates.
- **Session Persistence** — Intelligent JSONL-backed session management with project-aware storage, branching, forking, and tree visualization.
- **Flexible Modes** — TUI mode, one-shot JSON mode, or a headless RPC server.

---

## Getting Started

### Prerequisites

- **Go** 1.25+
- **Nix** (optional, recommended) — with flake support enabled

### Installation

```bash
# Recommended: use Nix for a fully reproducible dev environment
nix develop

# Build binary with Go
go build -o glm ./cmd/glm

# Or install globally
go install ./cmd/glm
```

### Quick Start

```bash
# Launch the interactive TUI
glm

# One-shot answer (JSONL output)
glm --mode json "What is the best way to structure a Go project?"

# Resume the most recent session on startup
glm --continue

# Headless JSONL RPC server
glm --mode rpc
```

---

## Usage Modes

### 1. TUI Mode (default / `--mode tui`)

A rich, Bubble Tea-powered TUI with real-time streaming, tool cards, and session management.

#### Input

| Key | Action |
|---|---|
| `Enter` | Send message (or **Steer** the running agent) |
| `Shift+Enter` | Insert newline |
| `Ctrl+Enter` | Queue **follow-up** message (runs after agent finishes) |
| `Ctrl+C` | Abort the current agent run / clear input |
| `Esc` | Cancel streaming / close modal |
| `Ctrl+O` | Toggle tool call output expansion |
| `Ctrl+P` | Cycle to the next model (from `--models` flag) |

#### Slash Commands

| Command | Description |
|---|---|
| `/new` | Start a fresh session |
| `/resume <id>` | Resume a session by ID (supports fuzzy search) |
| `/fork` | Branch current session into a new child |
| `/clone` | Duplicate current session (no parent link) |
| `/tree` | Open session tree — navigate, resume, or branch |
| `/import <path>` | Import a session from a JSONL file |
| `/export <path>` | Export the current session to a JSONL file |
| `/model <provider/model>` | Switch model mid-conversation |
| `/stats` | View session info and token usage |
| `/config` | View active configuration |
| `/context` | View context window usage |
| `/compact` | Manually compact the context |
| `/skill:<name> [args]` | Invoke a skill |
| `/prompt:<name>` | Expand a prompt template into the editor |
| `/exit` | Quit |

#### Session Tree (`/tree`)

- **↑/↓** — Navigate the tree
- **Enter** — Resume the selected session
- **B** — Branch (fork) from the selected session
- **Esc** — Close

#### Bang Commands

```bash
!ls -la         # Execute shell command, paste output into editor
!!cat README.md # Execute shell command, send output directly to agent
```

#### At-File Attachments

Type `@` in the input to fuzzy-search and attach file contents to your prompt.

### 2. JSON Mode (`--mode json`)

One-shot CLI for quick queries and shell pipelines with JSONL output:

```bash
cat main.go | glm --mode json "Refactor this to use interfaces"
glm --mode json "Summarize the last 10 git commits" --model anthropic/claude-opus-4-5
```

### 3. RPC Mode (`--mode rpc`)

Headless JSONL server over stdin/stdout. Ideal for editor integrations and external tooling.

---

## Built-in Tools

| Tool | Description |
|---|---|
| `read` | Read file contents with offset/limit support |
| `write` | Create or overwrite files |
| `edit` | Search-and-replace edits within files |
| `bash` | Execute shell commands |
| `grep` | Search file contents via regex |
| `ls` | List directory contents |
| `find` | Locate files using glob patterns |

---

## Session Management

Sessions are stored as JSONL files in a project-aware directory structure:

```
~/.gollm/sessions/
  --Users-alice-Projects-myapp--/
    2026-04-23T07-06-54_{uuid}.jsonl
    2026-04-23T09-12-11_{uuid}.jsonl
```

Each session tracks full message history, model, provider, thinking level, system prompt, and parent session ID (for branching). The `/tree` command visualizes the complete session hierarchy with box-drawing characters.

---

## Extensibility

### Skills

Drop `.md` files into `.gollm/skills/` or `~/.gollm/skills/` to add reusable instructions or personality. Invoke with `/skill:<name>`.

### Prompt Templates

Store reusable prompts in `.gollm/prompts/` or `~/.gollm/prompts/`. Expand into the editor with `/prompt:<name>`.

### Context Files

`gollm` auto-discovers `AGENTS.md`, `CLAUDE.md`, and `.gollm/context.md` in your project root and injects them into the system prompt.

### gRPC Extensions

Register external tools and lifecycle hooks via gRPC. See [`extensions/`](extensions/) for the protocol and loader.

---

## Configuration

`gollm` uses layered JSON configuration:

| Path | Scope |
|---|---|
| `~/.gollm/config.json` | Global defaults |
| `.gollm/config.json` | Project-level overrides |

```jsonc
{
  "model": "llama3.2",
  "provider": "ollama",
  "theme": "mocha",
  "thinkingLevel": "medium",
  "ollamaBaseURL": "http://localhost:11434",
  "openaiBaseURL": "https://api.openai.com/v1",
  "llamacppBaseURL": "http://localhost:8080",
  "compaction": {
    "enabled": true,
    "reserveTokens": 2048,
    "keepRecentTokens": 8192
  }
}
```

### CLI Flags

```
--model / -m         Model to use
--provider           Provider (ollama, openai, anthropic, llamacpp, google)
--thinking           Thinking level (off, low, medium, high)
--theme              UI theme (dark, mocha, light, …)
--session            Resume a specific session by ID or path
--continue / -c      Resume the most recent session
--mode               Mode: tui (default), json, rpc
--no-session         Disable session persistence
--models             Comma-separated model list for Ctrl+P cycling
```

---

## Go SDK

Embed a `gollm` agent directly in your Go application:

```go
import "gollm/sdk"

ag, err := sdk.NewAgent(sdk.Config{
    Provider: "ollama",
    Model:    "llama3.2",
    Tools:    sdk.DefaultTools(),
})
if err != nil { panic(err) }

ag.Subscribe(func(e sdk.Event) {
    if e.Type == sdk.EventTextDelta {
        fmt.Print(e.Content)
    }
})

ag.Prompt(context.Background(), "List the Go files in this directory")
<-ag.Idle()
```

---

## Development

```bash
# Enter the Nix dev shell
nix develop

# Build
go build ./...

# Test
go test ./...

# Run locally
go run ./cmd/glm
```

---

## License

BSD-3-Clause
