# glm — Architecture Overview

This document describes the high-level architecture of `gollm`: how its components are organized, how data flows through the system, and how the key abstractions relate to each other.

---

## Directory Structure

```
gollm/
│   ├── service/        # Central AgentService implementation + in-process client
│   ├── gen/            # Generated Protobuf stubs (pb.AgentServiceClient/Server)
│   ├── agent/          # Core agentic loop, event bus, state machine
│   ├── llm/            # LLM provider adapters (Ollama, OpenAI, Anthropic, llama.cpp, Google)
│   ├── tools/          # Built-in tool implementations + registry
│   ├── session/        # JSONL-backed session persistence, branching, tree
│   ├── modes/
│   │   ├── interactive/ # Bubble Tea TUI (pb client)
│   │   ├── print.go    # One-shot CLI JSONL mode (pb client)
│   │   └── grpc.go     # gRPC server mode (wraps Service)
│   ├── config/         # Config loading (global + project layering)
│   ├── themes/         # TUI colour themes
│   ├── types/          # Shared value types (Message, Session, ThinkingLevel)
│   ├── events/         # Generic publish-subscribe event bus
│   ├── skills/         # Skill discovery (Markdown files → slash commands)
│   ├── prompts/        # Prompt template discovery
│   └── contextfiles/   # Auto-discovered context file injection (AGENTS.md, etc.)
└── proto/              # Protobuf definitions (gollm/v1/agent.proto)
└── extensions/         # gRPC extension loader + proto definitions
```

---

## Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│  CLI flags → Config → Backend Service Init                 │
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
          │  │  consumeStream()        ││         executes tools, loops
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

## Service Architecture

`gollm` follows a **Strict Protobuf Internal Architecture**. Instead of UI modes calling Go functions directly, all interfaces are treated as clients of a central `AgentService`.

### Protobuf Boundary

The interface between the UI and the core is defined in `proto/gollm/v1/agent.proto`. This boundary ensures:
- **Consistency**: All modes (TUI, CLI, JSON, Remote gRPC) use the exact same code paths and logic.
- **Decoupling**: UI logic is completely isolated from agent state, session persistence, and provider adapters.
- **Interoperability**: Any gRPC-capable client can interact with a `gollm` service.

### In-Process Communication

For local CLI usage, `gollm` uses a specialized **In-Process Client** (`internal/service/client.go`). It uses `bufconn` to implement the `pb.AgentServiceClient` interface over an in-memory pipe. This provides the safety and structure of gRPC without the latency or configuration complexity of network ports.

### Backend Service (`internal/service`)

The `Service` struct implements `pb.AgentServiceServer`. It owns the `session.Manager` and manages the lifecycle of `agent.Agent` instances. It translates between internal agent events (Go channels) and Protobuf event streams.

#### Session Loading Strategy

RPCs split into two lookup strategies:

| Strategy | Used by | Behaviour |
|---|---|---|
| `getOrCreate(id)` | `Prompt`, `NewSession` | Always returns an entry — creates a fresh agent if `id` is unknown, loading from disk if a matching session file exists |
| `loadIfExists(id)` | `GetState`, `GetMessages`, `ConfigureSession`, `ForkSession`, `CloneSession`, etc. | Returns the entry if it is in memory **or** can be loaded from disk; returns `NotFound` for completely unknown IDs |
| `lookup(id)` | `Steer`, `Abort`, `FollowUp`, `StreamEvents` | In-memory only — these only make sense for a currently-running agent |

This means a `/resume <id>` command can switch to any session ever saved to disk without a round-trip `NewSession` call: the first `GetMessages` or `GetState` call transparently loads it.

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

- **Recursion Depth (`MaxSteps`)**: The `runTurn` loop tracks steps and aborts with an error if the LLM exceeds the configured `MaxSteps`. This prevents "hallucination loops" or infinite tool chains.
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

- **Line 0 (header)**: `kind=header` — session ID, parentId, model, timestamps, system prompt, compaction settings, dryRun flag
- **Subsequent lines**: `kind=message` — individual conversation messages with full payloads (role, content, thinking, tool calls, tool call ID)

### Session Tree

Sessions form a **linked tree** via `parentId`. The `session.Manager.BuildTree()` method assembles all sessions from the project directory into a `[]*TreeNode` tree. `FlattenTree` produces a depth-first flat list with structured layout metadata (gutters, connectors, indentation), which the TUI layer uses to render a clean Unicode box-drawing tree diagram.

### Branching, Rebasing & Merging
- **`/branch [index]`** — Creates a child session linked via `parentId`. If an index is provided, only messages up to that point are copied.
- **`/fork`** — Duplicates a session with no parent link (independent snapshot).
- **`/tree` → `B`** — Forks any session in the tree hierarchy on the fly.
- **`/rebase`** — Interactive mode to select a point in history to branch from, allowing for "what-if" scenarios or cleaning up conversation loops.
- **`/merge <id>`** — Append-only merge of another session's messages into the current context.
- **`/compact`** — Manually triggers a context compaction to free up tokens while preserving essential history.

---

## TUI Mode (tui)

The TUI is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (v2) and organized into focused files:

| File | Responsibility |
|---|---|
| `interactive.go` | `Run()` entry point, gRPC client wiring |
| `model.go` | `model` struct definition, `newModel()` |
| `update.go` | `Update()` — key handling, slash commands, picker logic, `promptGRPC()` |
| `events.go` | `handleAgentEvent()` — maps `*pb.AgentEvent` payloads to TUI history updates |
| `view.go` | `View()` — renders chat history, status bar, input |
| `modal.go` | Stats, Config, and Session Tree modal overlays |
| `slash.go` | Slash command parsing and handlers (all via gRPC client) |
| `picker.go` | Fuzzy picker component (sessions, skills, files, prompts) |
| `keys.go` | Keybinding helpers (`Matches`, `K.Ctrl(...)`) |
| `types.go` | `historyEntry`, `contentItem`, `toolCallEntry` — render data model |
| `utils.go` | Helper functions (`Capitalize`) |

### Prompt Submission

Prompt submission in the TUI uses `promptGRPC()`, which opens a `client.Prompt()` server-streaming RPC and drains `*pb.AgentEvent` messages into `m.eventCh` in a goroutine. The `listenForEvent` Bubble Tea command feeds that channel back into the update loop one event at a time.

### Prompt History

The TUI maintains a per-session prompt history in `m.promptHistory`, synced from the service via `GetMessages` at startup and after session switches. Users navigate previous prompts using **Up/Down** arrow keys while the editor is focused; the current draft is preserved as `m.draftInput`.

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

This mirrors the `content[]` array model, ensuring correct temporal ordering of thinking, text, and tool calls.

### Modal System
- **Stats** — Token counts, session metadata, file/path info
- **Config** — Active model, provider, compaction settings
- **Session Tree** — Interactive paginated tree with structured branch visualization; supports Resume (`Enter`) and Branch (`B`)
- **Rebase Picker** — Selection interface for history manipulation
- **Merge Picker** — Fuzzy finder for selecting sessions to merge into the current conversation

---

## Extensions

Extensions implement the `agent.Extension` interface:

```go
type Extension interface {
    // Name returns the extension's unique identifier.
    Name() string

    // Tools returns additional tools to register with the agent.
    Tools() []tools.Tool

    // BeforePrompt is called before each LLM request.
    BeforePrompt(ctx context.Context, state *AgentState) *AgentState

    // BeforeToolCall is called before each tool execution.
    // Return (result, true) to intercept; (nil, false) to allow normal execution.
    BeforeToolCall(ctx context.Context, call *ToolCall, args json.RawMessage) (*tools.ToolResult, bool)

    // AfterToolCall is called after each tool call completes.
    AfterToolCall(ctx context.Context, call *ToolCall, result *tools.ToolResult) *tools.ToolResult

    // ModifySystemPrompt augments the system prompt before each turn.
    ModifySystemPrompt(prompt string) string
}
```

Two extension types are supported:

1. **gRPC extensions** (`extensions/grpc.go`) — External processes connected via hashicorp/go-plugin gRPC. The loader launches the process, performs the handshake, and wraps its tools as native `Tool` implementations. Use `extensions.HandshakeConfig`, `extensions.ExtensionPlugin`, and `extensions.NoopPlugin` from `github.com/goppydae/gollm/extensions`. Plugin tools declare read-only semantics via the `IsReadOnly bool` field on `extensions.ToolDefinition`; this propagates to the internal `RemoteTool.IsReadOnly()` so dry-run mode and sandbox extensions can honour it correctly.
2. **Skills** (`internal/skills`) — Markdown files discovered from `.gollm/skills/` that are injected into the system prompt or sent as user messages via `/skill:<name>`.

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

The SDK re-exports core types (`Agent`, `Event`, `EventType`, `Tool`, `ThinkingLevel`, `Extension`) so consumers only need to import `gollm/sdk`.

---

## Build & Release System

`gollm` uses a combination of **Mage** and **GitHub Actions** for CI/CD.

### Versioning

The project version is maintained in a [VERSION](../VERSION) file in the repository root. During build, `Magefile.go` reads this file and injects it into the binary using linker flags (`-ldflags "-X main.version=..."`).

### Build Tool (Mage)

The `Magefile.go` defines several targets:
- `Build`: Compiles the `glm` binary for the current platform with version injection.
- `Test`: Runs all unit tests with optional coverage support.
- `All`: Runs build, test, vet, lint, and vulnerability scan (`govulncheck`).
- `Release`: Cross-compiles `glm` for Linux, macOS, and Windows (AMD64/ARM64), disables CGO for static portability, and packages artifacts into compressed archives in `dist/`.
- `Generate`: Runs `buf` to regenerate protobuf stubs in `internal/gen/gollm/v1/` and `extensions/gen/`.

### CI/CD Pipelines

1. **Continuous Integration** (`ci.yml`): Triggered on every push to `main` and all pull requests. It runs `mage all` (build, test, vet, lint, govulncheck) within a Nix environment on both `ubuntu-latest` and `macos-latest`, then uploads per-platform binaries as build artifacts. Coverage is also collected and summarised via `go tool cover`.
2. **Automated Release** (`release.yml`): Triggered by pushing a version tag (e.g., `v1.2.3`). It runs `mage release` to build cross-platform assets and uses `softprops/action-gh-release` to publish them to a new GitHub Release.

---

## Data Flow Summary

```
User Input
    ↓
[TUI (tui) / JSON (json) / Remote Client]
    ↓
[pb.AgentServiceClient] (In-Process bufconn / TCP)
    ↓
[internal/service] (pb.AgentServiceServer)
  - getOrCreate / loadIfExists: load session from disk if needed
    ↓
agent.Prompt(ctx, text)
    ↓
runTurn loop
    ├── ext.ModifySystemPrompt() / ext.BeforePrompt()
    ├── llm.Provider.Stream()  → EventTextDelta / EventThinkingDelta / EventToolCall
    ├── ext.BeforeToolCall() / execTool() / ext.AfterToolCall()
    │        → EventToolDelta / EventToolOutput
    └── loop until no tool calls
    ↓
EventAgentEnd
    ↓
internal/service (saves session on AgentEnd)
    ↓
[Stream Protobuf Events to Client]
    ↓
[Render to TUI / JSONL stdout / Remote gRPC stream]
```
