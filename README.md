# gollm — local-first AI coding agent

**Primitives, not features. Local-first. Extensible.**

`gollm` is a powerful, local-first AI agentic harness designed for developers who want a flexible and reliable assistant that runs on their own hardware. It prioritizes local LLMs (via Ollama and llama.cpp) but adapts seamlessly to cloud providers like OpenAI, Anthropic, and Google Gemini.

> A Golem is designed to be a tireless servant to its creator. Brought to life through ritual, created entirely from inanimate matter. It performs physical labor or provides protection.

<div align="center">

[![CI](https://github.com/goppydae/gollm/actions/workflows/ci.yml/badge.svg)](https://github.com/goppydae/gollm/actions/workflows/ci.yml) [![Coverage](https://codecov.io/gh/goppydae/gollm/branch/main/graph/badge.svg)](https://codecov.io/gh/goppydae/gollm) [![Go Reference](https://pkg.go.dev/badge/github.com/goppydae/gollm.svg)](https://pkg.go.dev/github.com/goppydae/gollm) [![Go Report Card](https://goreportcard.com/badge/github.com/goppydae/gollm)](https://goreportcard.com/report/github.com/goppydae/gollm)

[![Latest Release](https://img.shields.io/github/v/release/goppydae/gollm)](https://github.com/goppydae/gollm/releases/latest) [![Go Version](https://img.shields.io/badge/go-1.26.2+-blue)](https://go.dev/dl/) [![License](https://img.shields.io/github/license/goppydae/gollm)](https://github.com/goppydae/gollm/blob/main/LICENSE)

</div>

---

## Core Philosophy

- **Local-First** — Built from the ground up to favor local inference for privacy, speed, and cost-efficiency.
- **Aggressively Extensible** — Every tool, provider, and behavior is a plugin interface. Supports gRPC extensions, markdown skills, and reusable prompt templates.
- **Session Persistence** — Intelligent JSONL-backed session management with project-aware storage, branching, forking, and tree visualization.
- **Flexible Modes** — TUI mode, one-shot mode, or a multi-session gRPC service—all powered by a central service-oriented architecture.
- **Security & Safety** — Dry-run safety for destructive tools, automatic prompt injection mitigation, and a gRPC extension system for enforcing arbitrary policies.

---

## Getting Started

### Prerequisites

- **Go** 1.26.2+
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
```

---

## Usage Modes

### 1. TUI Mode (default / `--mode tui`)

A rich, Bubble Tea-powered TUI with real-time streaming, tool cards, session management, and a live context usage progress bar in the status footer.

#### Input

| Key | Action |
|---|---|
| `Enter` | Send message (or **Steer** the running agent) |
| `Shift+Enter` | Insert newline |
| `Ctrl+Enter` | Queue **follow-up** message (runs after agent finishes) |
| `Ctrl+C` | Abort the current agent run and clear the input editor |
| `Esc` | Cancel streaming / Close modal / Abort current turn |
| `Ctrl+O` | Toggle tool call output expansion |
| `Ctrl+P` | Open model selection modal (cycling via `--models` flag) |
| `↑/↓` | Navigate prompt history (if at start/end of editor) / Scroll viewport |
| `F1` | Show help modal |

#### Slash Commands

| Command | Description |
|---|---|
| `/new` | Start a fresh session |
| `/resume <id>` | Resume a session by ID or partial UUID (fuzzy search enabled) |
| `/branch [idx]` | Create a new child session branching from a specific message index (defaults to last) |
| `/fork` | Duplicate current session into a new independent session (no parent link) |
| `/rebase` | Interactive rebase: select specific messages to keep in a new session |
| `/merge <id>` | Merge another session's history into the current one with a synthesis turn |
| `/tree [-g\|-p]` | Open session tree modal. Flags: `--global` (-g) or `--project` (-p) |
| `/import <path>` | Import a session from a JSONL file |
| `/export <path>` | Export the current session to a JSONL file |
| `/model <p/m>` | Switch model mid-conversation (e.g. `/model anthropic/claude-3-5-sonnet`) |
| `/stats` | View session statistics and token usage |
| `/config` | View and edit active configuration |
| `/context` | View detailed context window usage |
| `/compact` | Manually trigger a context compaction |
| `/skill:<name> [args]` | Invoke a skill |
| `/prompt:<name>` | Expand a prompt template into the editor |
| `/exit` | Quit (alias: `/quit`) |

#### Session Tree Modal (`/tree`)

- **↑/↓ / PgUp/PgDn** — Navigate the session list
- **Enter** — Resume the selected session (or branch from it if it's an interior node)
- **B** — Create a new **branch** from the selected session
- **F** — Create an independent **fork** of the selected session
- **R** — Start an interactive **rebase** from the selected session's history
- **Esc** — Close modal

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

Each event is emitted as a single JSON line using the protobuf JSON encoding of `AgentEvent`.

### 3. gRPC Mode (`--mode grpc`)

A persistent multi-session gRPC service. The CLI acts as a client to a central `AgentService`, either in-process or over the network. Each client-supplied `session_id` gets its own isolated agent; sessions are saved to disk after each turn and reloaded automatically on reconnect.

```bash
# Start on the default port (:50051)
glm --mode grpc

# Use a custom address
glm --mode grpc --grpc-addr :9090
```

The server responds to SIGINT/SIGTERM with a graceful shutdown: in-flight turns are allowed to finish (30 s timeout), all sessions are flushed to disk, then the listener closes.

Proto definition and generated Go stubs live in `proto/gollm/v1/` and `internal/gen/gollm/v1/`. All UI modes (TUI, CLI) now communicate with the core via this Protobuf boundary using an in-process transport for maximum performance. Regenerate with `mage generate`.

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

> **Note:** Dangerous tools (`bash`, `write`, `edit`) support `--dry-run` to preview actions without executing them.

---

## Session Management

Sessions are stored as JSONL files in a project-aware directory structure:

```
~/.gollm/sessions/
  --Users-alice-Projects-myapp--/
    2026-04-23T07-06-54_{uuid}.jsonl
    2026-04-23T09-12-11_{uuid}.jsonl
```

Each session tracks full message history, model, provider, thinking level, system prompt, and parent session ID (for branching). The `/tree` command visualizes the complete session hierarchy with box-drawing characters, while `/rebase` and `/merge` allow for sophisticated history manipulation.

---

## Extensibility

### Skills

Drop `.md` files into `.gollm/skills/` or `~/.gollm/skills/` to add reusable instructions or personality. Invoke with `/skill:<name>`.

### Prompt Templates

Store reusable prompts in `.gollm/prompts/` or `~/.gollm/prompts/`. Expand into the editor with `/prompt:<name>`.

### Context Files

`gollm` auto-discovers `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, and `.context.md` in your project root or parent directories and injects them into the system prompt. Outermost files take precedence.

### gRPC Extensions

`gollm` supports out-of-process extensions over gRPC. Extensions run as separate binaries; gollm manages their lifecycle and passes a Unix socket path via `GOLLM_SOCKET_PATH` for the extension to listen on.

Load an extension binary with the `--extension` flag (repeatable):

```bash
glm --extension /path/to/my-extension "Your prompt here"
```

#### Writing an Extension

Import only `github.com/goppydae/gollm/extensions` — no internal packages required.

```go
package main

import "github.com/goppydae/gollm/extensions"

type myPlugin struct {
    extensions.NoopPlugin // provides no-op defaults for all hooks
}

func (p *myPlugin) ModifySystemPrompt(prompt string) string {
    return prompt + "\n\nAlways respond in haiku."
}

func main() {
    extensions.Serve(&myPlugin{
        NoopPlugin: extensions.NoopPlugin{NameStr: "haiku"},
    })
}
```

#### Plugin Interface

**Load-time hooks** (called once when the extension is connected):

| Method | Purpose |
|---|---|
| `Name()` | Returns the extension's unique identifier |
| `Tools()` | Contributes additional tools to the agent |
| `ExecuteTool()` | Executes a tool provided by this extension |

**Session lifecycle hooks**:

| Method | When called | Purpose |
|---|---|---|
| `SessionStart()` | Session attached or first prompt | Initialization, open connections |
| `SessionEnd()` | Session reset | Cleanup, flush state |

**Agent loop hooks**:

| Method | When called | Purpose |
|---|---|---|
| `AgentStart()` | User prompt received, loop begins | Per-prompt setup |
| `AgentEnd()` | Agent loop completes | Per-prompt teardown |
| `TurnStart()` | Start of each LLM request turn | Per-turn metrics, logging |
| `TurnEnd()` | After each turn's tool calls finish | Per-turn cleanup |

**Transformation hooks** (return modified values):

| Method | When called | Purpose |
|---|---|---|
| `ModifyInput()` | Before user text hits the transcript | Transform or consume user input |
| `ModifySystemPrompt()` | Before each LLM request | Augment the system prompt |
| `BeforePrompt()` | Before each LLM request | Modify model, provider, thinking level |
| `ModifyContext()` | Before each LLM request is built | Filter or inject messages sent to the LLM |
| `BeforeProviderRequest()` | Just before the request is sent | Modify temperature, max tokens, tools list |
| `AfterProviderResponse()` | After LLM stream consumed | Observe response content and tool call count |
| `BeforeToolCall()` | Before each tool execution | **Intercept or block tool calls** |
| `AfterToolCall()` | After each tool execution | Modify or observe tool results |
| `BeforeCompact()` | Before LLM-based summarization | **Provide a custom compaction summary** |
| `AfterCompact()` | After compaction completes | Observe freed token count |

Key behaviors:
- `ModifyInput` returns an `InputResult` with action `continue` (pass through), `transform` (replace text), or `handled` (consume without processing).
- `ModifyContext` receives and returns the full message slice that will be sent to the LLM — changes do not affect the stored transcript.
- `BeforeCompact` returns `nil` to let the default LLM summarization run, or a `*CompactionResult` to supply a custom summary and skip the LLM call entirely.
- `BeforeToolCall` returns `(result, true)` to intercept, or `(ToolResult{}, false)` to allow normal execution.

#### Example: Sandbox Extension

[`examples/sandbox/`](examples/sandbox/) is a complete, standalone gRPC extension that confines all file-system tool calls to the directory `glm` is started in. It is its own Go module and serves as a reference implementation.

```bash
# Build
cd examples/sandbox && go build -o gollm-sandbox .

# Use — all file access outside $PWD is blocked
glm --extension ./gollm-sandbox "Refactor main.go"
```

---

## Security & Safety

`gollm` is designed to be a safe and predictable assistant:

- **Dry Run Mode** — Use `--dry-run` to see what the agent *would* do without modifying files or running shell commands.
- **Bash Deny Patterns** — The `Bash` tool accepts a `DenyPatterns` list; matching commands are rejected before execution.
- **API Key Hygiene** — API keys are sent via request headers (never embedded in URLs) and masked in the `/config` modal to prevent accidental leaks.
- **Prompt Sanitization** — User-supplied arguments in prompt templates are wrapped in `<untrusted_input>` tags to mitigate injection attacks.
- **Extension Sandboxing** — The `BeforeToolCall` hook lets extensions enforce arbitrary path or command policies. See the [sandbox example](examples/sandbox/) for a reference implementation.

---

## Configuration

`gollm` uses layered JSON configuration:

| Path | Scope |
|---|---|
| `~/.gollm/config.json` | Global defaults |
| `.gollm/config.json` | Project-level overrides |

```jsonc
{
  "defaultModel": "llama3.2",
  "defaultProvider": "ollama",
  "theme": "dark",
  "thinkingLevel": "medium",
  "ollamaBaseURL": "http://localhost:11434",
  "openAIBaseURL": "https://api.openai.com/v1",
  "openAIApiKey": "",
  "anthropicApiKey": "",
  "anthropicApiVersion": "",
  "googleApiKey": "",
  "llamaCppBaseURL": "http://localhost:8080",
  "compaction": {
    "enabled": true,
    "reserveTokens": 2048,
    "keepRecentTokens": 8192
  }
}
```

### CLI Flags

```
Mode
  --mode               Mode: tui (default), json, grpc
  --grpc-addr          gRPC listen address (default :50051; --mode grpc only)

Model / Provider
  --model / -m         Model to use (e.g. llama3, gpt-4o, anthropic/claude-sonnet-4-6)
  --provider           Provider: ollama, openai, anthropic, llamacpp, google
  --api-key            API key override (env vars take priority otherwise)
  --thinking           Thinking level: off, minimal, low, medium, high, xhigh
  --models             Comma-separated model list for Ctrl+P cycling

Session
  --continue / -c      Resume the most recent session
  --resume / -r        Select a session to resume (fuzzy search or ID)
  --session            Use a specific session file path
  --session-dir        Directory for session storage and lookup
  --branch             Branch from a session file or partial UUID into a new child session
  --fork               Deprecated: use --branch
  --no-session         Ephemeral mode: don't save the session

System Prompt
  --system-prompt      Override the system prompt
  --append-system-prompt   Append text or file to the system prompt (repeatable)

Tools
  --tools              Comma-separated list of tools to enable (read,bash,edit,write,grep,find,ls)
  --no-tools           Disable all built-in tools
  --dry-run            Safety mode: destructive tools preview actions instead of running

Extensions / Skills / Prompts
  --extension / -e     Load a gRPC extension binary (repeatable)
  --no-extensions      Disable extension directory auto-discovery (-e paths still load)
  --skill              Load a skill file or directory (repeatable)
  --no-skills          Disable skill auto-discovery
  --prompt-template    Load a prompt template file or directory (repeatable)
  --no-prompt-templates  Disable prompt template auto-discovery

Context Files
  --no-context-files   Disable AGENTS.md / CLAUDE.md / GEMINI.md / .context.md auto-discovery

Theme
  --theme              UI theme name: dark, light, cyberpunk, synthwave, …
  --theme-path         Load a theme file or directory (repeatable)
  --no-themes          Disable theme auto-discovery

Output / Info
  --export             Export current session to an HTML file and exit
  --list-models        List available models from the configured provider (optional fuzzy filter)
  --version / -v       Show version number
  --verbose            Force verbose startup output
  --offline            Disable startup network operations (model checks, etc.)
```

---

## Go SDK

Embed a `gollm` agent directly in your Go application:

```go
import "github.com/goppydae/gollm/sdk"

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

Extensions are attached via `ag.SetExtensions()`. Use `sdk.NoopExtension` (aliased from `agent.NoopExtension`) as a base and override only the hooks you need:

```go
type loggingExt struct{ agent.NoopExtension }

func (e *loggingExt) AgentStart(ctx context.Context) { log.Println("agent started") }
func (e *loggingExt) AgentEnd(ctx context.Context)   { log.Println("agent finished") }
func (e *loggingExt) ModifyInput(ctx context.Context, text string) sdk.InputResult {
    if text == "quit" {
        return sdk.InputResult{Action: sdk.InputHandled} // consume without processing
    }
    return sdk.InputResult{Action: sdk.InputContinue}
}

ag.SetExtensions([]sdk.Extension{&loggingExt{NoopExtension: agent.NoopExtension{NameStr: "logger"}}})
```

---

## Development

`gollm` uses [Mage](https://magefile.org/) as its build system and [Nix](https://nixos.org/) for environment management.

```bash
# Enter the Nix dev shell (includes Go, buf, Mage, etc.)
nix develop

# Build the glm binary (uses VERSION file for injection)
mage build

# Run tests
mage test

# Run all checks (build, test, vet, lint, vuln)
mage all

# Regenerate protobuf stubs (buf, covers all targets)
mage generate

# Scan for known vulnerabilities in dependencies
mage vuln

# Create cross-platform release artifacts (in dist/)
mage release
```

---

## License

BSD-3-Clause
