# glm — Architecture Overview

This document describes the high-level architecture of `gollm`: how its components are organized, how data flows through the system, and how the key abstractions relate to each other.

---

## Directory Structure

```
gollm/
├── cmd/glm/           # Entry point — CLI flags, config loading, mode dispatch (--mode tui|json|grpc)
├── sdk/                # Public Go SDK (thin wrapper over internal/agent)
├── internal/
│   ├── agent/          # Core agentic loop, event bus, state machine
│   ├── llm/            # LLM provider adapters (Ollama, OpenAI, Anthropic, llama.cpp, Google)
│   ├── tools/          # Built-in tool implementations + registry
│   ├── session/        # JSONL-backed session persistence, branching, tree
│   ├── modes/
│   │   ├── interactive/ # Bubble Tea TUI (mode: tui)
│   │   └── print.go    # One-shot CLI JSONL mode (mode: json)
│   ├── config/         # Config loading (global + project layering)
│   ├── themes/         # TUI colour themes
│   ├── types/          # Shared value types (Message, Session, ThinkingLevel)
│   ├── events/         # Generic publish-subscribe event bus
│   ├── skills/         # Skill discovery (Markdown files → slash commands)
│   ├── prompts/        # Prompt template discovery
│   └── contextfiles/   # Auto-discovered context file injection (AGENTS.md, etc.)
└── extensions/         # gRPC extension loader + proto definitions
```

---

## Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│  CLI flags → Config → Mode dispatch (tui/json/grpc)        │
└────────────────────────┬────────────────────────────────────┘
                         │
          ┌──────────────▼──────────────┐
          │          internal/agent      │
          │  ┌─────────────────────────┐│
          │  │    Agent (core state)   ││
          │  │  - Messages []Message   ││
          │  │  - SteerQueue           ││
          │  │  - FollowUpQueue        ││
          │  │  - StateMachine         ││
          │  └────────────┬────────────┘│
          │               │             │
          │  ┌────────────▼────────────┐│
          │  │    runTurn (loop.go)    ││  ←──── drains queues, handles compaction,
          │  │  provider.Stream()      ││         execs extensions, calls LLM,
          │  │  consumeStream()           ││         executes tools, loops
          │  │  execTools()            ││
          │  └────────────┬────────────┘│
          │               │ publishes   │
          │  ┌────────────▼────────────┐│
          │  │       EventBus          ││  →  subscribers (TUI, gRPC, session saver)
          │  └─────────────────────────┘│
          └─────────────────────────────┘
                         │
          ┌──────────────▼──────────────┐
          │         internal/llm         │
          │  Provider interface:         │
          │    Stream(ctx, req) stream   │
          │    Info() ProviderInfo       │
          │                              │
          │  Adapters: Ollama, OpenAI,   │
          │  Anthropic, llama.cpp, Google│
          └──────────────────────────────┘
```

---

## Agent Lifecycle & Events

The agent is driven by an **event-bus** (`internal/events`). Every meaningful state transition emits an `agent.Event` to all subscribers. The TUI and session saver each subscribe independently.

### Event Flow

```
agent.Prompt(ctx, text)
  → EventAgentStart
  → EventTurnStart
  → EventMessageStart
  → EventTextDelta* / EventThinkingDelta* / EventToolCall*
  → EventMessageEnd
  → [tool execution]
       → EventToolDelta* (streaming output)
       → EventToolOutput (final result)
  → [loop again if tool calls present]
  → EventAgentEnd
```

### State Machine

The agent transitions through explicit states to prevent concurrent modification:

```
Idle → Thinking → Executing → Idle
           ↓
       Compacting → Idle
           ↓
         Aborting → Idle
           ↓
         Error
```

### Prompt Queues

Two queues support non-blocking interaction while the agent is running:

- **SteerQueue** — Injected as a user message at the next tool boundary (interrupt-style)
- **FollowUpQueue** — Processed as a new turn after the agent goes Idle

---

## LLM Provider Interface

```go
type Provider interface {
    Stream(ctx context.Context, req Request) (Stream, error)
    Info() ProviderInfo
}
```

All providers return a uniform `Stream` of `Chunk` values — text deltas, thinking deltas, tool calls, and usage. The agent's `consumeStream` function normalizes this into the internal `Message` format.

**Supported providers:**

| Provider | Backend |
|---|---|
| `ollama` | Local Ollama server (HTTP) |
| `llamacpp` | llama.cpp server (HTTP, OpenAI-compatible) |
| `openai` | OpenAI API or any OpenAI-compatible endpoint |
| `anthropic` | Anthropic Messages API |
| `google` | Google Gemini API |

---

## Tool System

Tools implement a simple interface:

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage, call *ToolCall) (ToolResult, error)
}
```

A `ToolRegistry` holds all registered tools. During a turn, when the LLM emits a tool call, `execTool` looks up the tool by name, executes it, and streams partial output via `EventToolDelta` before emitting the final `EventToolOutput`.

**Built-in tools:** `read`, `write`, `edit`, `bash`, `grep`, `ls`, `find`

### Security & Safety Enforcements

The tool system enforces several safety layers:

- **Recursion Depth (`MaxSteps`)**: The `runTurn` loop tracks steps and aborts with an error if the LLM exceeds the configured `MaxSteps` (default: 10). This prevents "hallucination loops" or infinite tool chains.
- **Dry-Run Mode**: When `DryRun` is enabled, any tool that is not marked as read-only will bypass execution and return a descriptive preview of what it *would* have done.
- **Input Sanitization**: Prompt template expansion automatically wraps user inputs in `<untrusted_input>` tags to prevent prompt breakout and injection into the base instructions.

---

## Session Management

Sessions are persisted as **JSONL files** in a project-aware directory:

```
~/.gollm/sessions/
  --Users-alice-Projects-myapp--/     ← sanitized CWD
    2026-04-23T07-06-54_{uuid}.jsonl  ← timestamped session file
    2026-04-23T09-12-11_{uuid}.jsonl
```

### Session File Format

Each `.jsonl` file contains one JSON object per line:

- **Line 0 (header)**: `kind=header` — session ID, parentId, model, timestamps, system prompt
- **Subsequent lines**: `kind=message` — individual conversation messages with full payloads

### Session Tree

Sessions form a **linked tree** via `parentId`. The `session.Manager.BuildTree()` method assembles all sessions from the project directory into a `[]*TreeNode` tree. `FlattenTree` produces a depth-first flat list with structured layout metadata (gutters, connectors, indentation), which the TUI layer uses to render a clean Unicode box-drawing tree diagram.

### Branching & Forking

- **`/fork`** — Creates a child session copying all messages and metadata; `parentId` is set.
- **`/clone`** — Duplicates a session with no parent link.
- **`/tree` → `B`** — Forks any session in the tree hierarchy on the fly.

---

## TUI Mode (tui)

The TUI is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (v2) and organized into focused files:

| File | Responsibility |
|---|---|
| `interactive.go` | `Run()` entry point, agent and session wiring |
| `model.go` | `model` struct definition |
| `update.go` | `Update()` — key handling, slash commands, picker logic |
| `events.go` | `handleAgentEvent()` — maps agent events to TUI history updates |
| `view.go` | `View()` — renders chat history, status bar, input |
| `modal.go` | Stats, Config, and Session Tree modal overlays |
| `slash.go` | Slash command parsing and handlers |
| `picker.go` | Fuzzy picker component (sessions, skills, files, prompts) |
| `keys.go` | Keybinding helpers (`Matches`, `K.Ctrl(...)`) |
| `types.go` | `historyEntry`, `contentItem`, `toolCallEntry` — render data model |
| `utils.go` | Helper functions |

### Prompt History

The TUI maintains a per-session prompt history in `m.promptHistory`. Users can navigate through previous prompts using the **Up** and **Down** arrow keys while the editor is focused. The current draft is preserved as `m.draftInput` when navigating away from the prompt line. The status footer also includes a real-time **context window progress bar** driven by token usage events from the agent.

### Render Data Model

The TUI stores conversation history as `[]historyEntry`. Each entry has an ordered `[]contentItem` slice that preserves the exact stream order:

```
historyEntry {
  role: "assistant"
  items: [
    { kind: contentItemThinking, text: "..." }
    { kind: contentItemText,     text: "..." }
    { kind: contentItemToolCall, tc: { id, name, arg, status, streamingOutput } }
    { kind: contentItemToolOutput, out: { toolCallID, content, isError } }
  ]
}
```

This mirrors pi-mono's `content[]` array model, ensuring correct temporal ordering of thinking, text, and tool calls.

### Modal System

Three modal overlays share a `modalState` struct:
- **Stats** — Token counts, session metadata, file/path info
- **Config** — Active model, provider, compaction settings
- **Session Tree** — Interactive paginated tree with structured branch visualization; supports Resume (`Enter`) and Branch (`B`)

---

## Extensions

Extensions implement the `agent.Extension` interface:

```go
type Extension interface {
    ModifySystemPrompt(ctx context.Context, state *AgentState) *AgentState
    BeforePrompt(ctx context.Context, state *AgentState) *AgentState
    AfterToolCall(ctx context.Context, state *AgentState) *AgentState
    Tools() []tools.Tool
}
```

Two extension types are supported:

1. **gRPC extensions** (`extensions/grpc.go`) — External processes connected via gRPC. The loader launches the process and wraps its tools as native `Tool` implementations.
2. **Skills** (`extensions/skills.go`) — Markdown files discovered from `.gollm/skills/` that are injected into the system prompt or sent as user messages via `/skill:<name>`.

---

## Go SDK

`gollm/sdk` exposes a thin public API over `internal/agent`, intended for embedding an agent in other Go programs:

```go
ag, _ := sdk.NewAgent(sdk.Config{
    Provider: "ollama",
    Model:    "llama3.2",
    Tools:    sdk.DefaultTools(),
})
ag.Subscribe(func(e sdk.Event) { ... })
ag.Prompt(ctx, "Hello")
<-ag.Idle()
```

The SDK re-exports core types (`Agent`, `Event`, `EventType`, `Tool`, `ThinkingLevel`) so consumers only need to import `gollm/sdk`.

---

## Build & Release System

`gollm` uses a combination of **Mage** and **GitHub Actions** for CI/CD.

### Versioning

The project version is maintained in a [VERSION](file:///Users/sysop/Projects/giggle-silo/gollm/VERSION) file in the repository root. During build, `Magefile.go` reads this file and injects it into the binary using linker flags (`-ldflags "-X main.version=..."`).

### Build Tool (Mage)

The `Magefile.go` defines several targets:
- `Build`: Compiles the `glm` binary for the current platform with version injection.
- `Test`: Runs all unit tests with optional coverage support.
- `Release`: Cross-compiles `glm` for Linux, macOS, and Windows (AMD64/ARM64), disables CGO for static portability, and packages artifacts into compressed archives in `dist/`.

### CI/CD Pipelines

1. **Continuous Integration** (`ci.yml`): Triggered on every push to `main` and all pull requests. It runs `mage all` (build, test, vet, lint) within a Nix environment and uploads the resulting binary as a build artifact.
2. **Automated Release** (`release.yml`): Triggered by pushing a version tag (e.g., `v1.2.3`). It runs `mage release` to build cross-platform assets and uses `softprops/action-gh-release` to publish them to a new GitHub Release.

---

## Data Flow Summary

```
User Input
    ↓
[TUI (tui) / JSON (json) / gRPC (grpc)]
    ↓
agent.Prompt(ctx, text)
    ↓
runTurn loop
    ├── llm.Provider.Stream()  → EventTextDelta / EventThinkingDelta / EventToolCall
    ├── execTool()             → EventToolDelta / EventToolOutput
    └── loop until no tool calls
    ↓
EventAgentEnd
    ↓
session.Manager.Save()         ← TUI subscriber saves on AgentEnd
    ↓
[Render to TUI / JSONL / gRPC stream]
```
