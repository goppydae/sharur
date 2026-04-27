# Tutorial: Creating Extensions

Extensions let you add new behaviors to `gollm` beyond what's possible with skills and prompt templates. They can modify the system prompt before every turn, add custom tools the agent can call, and intercept or observe tool results. Extensions run as separate processes and communicate with `gollm` via gRPC.

---

## Extension Types

| Type | Language | Use Case |
|---|---|---|
| **Go binary** | Go | High-performance tools, direct filesystem access |
| **Python script** | Python | Data processing, ML integrations, API calls |
| **Any executable** | Any | Shell scripts, compiled binaries from any language |

All extension types use the same gRPC protocol. The loader treats `.py` files specially (runs them with the configured Python interpreter), and everything else is executed directly as a binary.

---

## Extension Discovery Directories

Extensions are loaded from directories listed in your config under `extensions`:

```jsonc
// .gollm/config.json
{
  "extensions": [".gollm/extensions"]
}
```

Or globally in `~/.gollm/config.json`.

Place your extension binary or script in the configured directory. `gollm` will automatically discover and launch it on startup.

You can also load a specific extension at runtime with the `--extension` flag:

```bash
glm --extension /path/to/my-extension "Your prompt here"
```

---

## The Plugin Interface

Every Go extension implements the `extensions.Plugin` interface from `github.com/goppydae/gollm/extensions`:

```go
type Plugin interface {
    Name() string
    Tools() []ToolDefinition
    ExecuteTool(ctx context.Context, name string, args json.RawMessage) ToolResult
    ModifySystemPrompt(prompt string) string
    BeforePrompt(ctx context.Context, state AgentState) AgentState
    BeforeToolCall(ctx context.Context, call ToolCall, args json.RawMessage) (ToolResult, bool)
    AfterToolCall(ctx context.Context, call ToolCall, result ToolResult) ToolResult
}
```

| Method | When called | Purpose |
|---|---|---|
| `Name` | On load | Returns the extension's identifier string |
| `Tools` | On load | Returns tool definitions the agent can call |
| `ExecuteTool` | On tool call | Executes a tool registered by this extension |
| `ModifySystemPrompt` | Before each turn | Augment or replace the system prompt |
| `BeforePrompt` | Before each LLM call | Modify agent state (model, prompt, etc.) |
| `BeforeToolCall` | Before each tool execution | **Intercept or block tool calls** |
| `AfterToolCall` | After each tool execution | Observe or modify tool results |

`BeforeToolCall` is the interception point: return `(result, true)` to prevent the built-in tool from running and substitute your own result; return `(ToolResult{}, false)` to allow normal execution.

You only need to override the methods relevant to your extension. Use `extensions.NoopPlugin` as a base to get no-op defaults for everything else.

---

## The Handshake

Extensions must use the [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) handshake provided by the `extensions` package:

```go
extensions.HandshakeConfig  // plugin.HandshakeConfig value to pass to plugin.Serve
extensions.ExtensionPlugin  // the plugin.Plugin implementation — set Impl to your Plugin
```

---

## Example: Go Extension

### 1. Create the project structure

```
.gollm/extensions/
  git-context/           # extension directory
    main.go
    go.mod
```

### 2. Implement the extension

```go
// .gollm/extensions/git-context/main.go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"

    goplugin "github.com/hashicorp/go-plugin"
    "github.com/goppydae/gollm/extensions"
)

// GitContextPlugin injects the current git branch and recent commits
// into the system prompt before each turn.
type GitContextPlugin struct {
    extensions.NoopPlugin
}

func (p *GitContextPlugin) BeforePrompt(_ context.Context, state extensions.AgentState) extensions.AgentState {
    branch := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
    status := gitOutput("status", "--short")
    log := gitOutput("log", "--oneline", "-5")

    state.SystemPrompt += fmt.Sprintf(
        "\n\n<git_context>\nBranch: %s\n\nRecent commits:\n%s\n\nWorking tree:\n%s\n</git_context>",
        branch, log, status,
    )
    return state
}

func gitOutput(args ...string) string {
    out, err := exec.Command("git", args...).Output()
    if err != nil {
        return "(unavailable)"
    }
    return strings.TrimSpace(string(out))
}

func main() {
    goplugin.Serve(&goplugin.ServeConfig{
        HandshakeConfig: extensions.HandshakeConfig,
        Plugins: goplugin.PluginSet{
            "extension": &extensions.ExtensionPlugin{Impl: &GitContextPlugin{
                NoopPlugin: extensions.NoopPlugin{NameStr: "git-context"},
            }},
        },
        GRPCServer: goplugin.DefaultGRPCServer,
    })
}
```

### 3. Build the extension

```bash
cd .gollm/extensions/git-context
go build -o ../git-context .
```

### 4. Configure and run

```jsonc
// .gollm/config.json
{
  "extensions": [".gollm/extensions"]
}
```

The `git-context` binary in `.gollm/extensions/` will be auto-discovered and loaded.

---

## Example: Extension with Custom Tools

Extensions can contribute tools the agent calls just like built-in tools:

```go
type CounterPlugin struct {
    extensions.NoopPlugin
}

func (p *CounterPlugin) Tools() []extensions.ToolDefinition {
    return []extensions.ToolDefinition{
        {
            Name:        "count_lines",
            Description: "Count lines in a string",
            Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
            IsReadOnly:  true, // read-only tools are allowed through dry-run mode and sandbox extensions
        },
    }
}

// ToolDefinition fields:
//   Name        — tool name as the LLM will call it
//   Description — shown to the LLM in its tool list
//   Schema      — JSON Schema object describing the input parameters
//   IsReadOnly  — true = tool never mutates state; dry-run mode and sandbox
//                 extensions use this to decide whether to allow the call

func (p *CounterPlugin) ExecuteTool(_ context.Context, name string, args json.RawMessage) extensions.ToolResult {
    if name != "count_lines" {
        return extensions.ToolResult{Content: "unknown tool", IsError: true}
    }
    var input struct{ Text string `json:"text"` }
    _ = json.Unmarshal(args, &input)
    n := strings.Count(input.Text, "\n") + 1
    return extensions.ToolResult{Content: fmt.Sprintf("%d lines", n)}
}
```

---

## Example: Intercepting Tool Calls (Sandbox)

`BeforeToolCall` lets you block or replace any built-in tool call:

```go
type SandboxPlugin struct {
    extensions.NoopPlugin
    AllowedDir string
}

func (p *SandboxPlugin) BeforeToolCall(_ context.Context, call extensions.ToolCall, args json.RawMessage) (extensions.ToolResult, bool) {
    // Block write/edit/bash for paths outside AllowedDir
    var input struct{ Path string `json:"path"` }
    _ = json.Unmarshal(args, &input)
    if input.Path != "" && !strings.HasPrefix(input.Path, p.AllowedDir) {
        return extensions.ToolResult{
            Content: fmt.Sprintf("blocked: %s is outside %s", input.Path, p.AllowedDir),
            IsError: true,
        }, true // true = intercept, don't run the real tool
    }
    return extensions.ToolResult{}, false // false = allow normal execution
}
```

[`examples/sandbox/`](../examples/sandbox/) is a complete, standalone implementation of this pattern.

---

## Example: Python Extension

Python extensions work the same way but are invoked with the Python interpreter. You'll need the `grpcio` and `grpcio-tools` Python libraries and the generated proto stubs.

### 1. Generate Python stubs

```bash
pip install grpcio grpcio-tools
python -m grpc_tools.protoc \
  -I extensions/proto \
  --python_out=.gollm/extensions \
  --grpc_python_out=.gollm/extensions \
  extensions/proto/extension.proto
```

### 2. Implement the extension

```python
# .gollm/extensions/ticket_context.py
import os
import subprocess
import grpc
from concurrent import futures
import extension_pb2
import extension_pb2_grpc


class TicketContextServicer(extension_pb2_grpc.ExtensionServicer):
    def Name(self, request, context):
        return extension_pb2.NameResponse(name="ticket-context")

    def Tools(self, request, context):
        return extension_pb2.ToolsResponse(tools=[])

    def BeforePrompt(self, request, context):
        branch = subprocess.check_output(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"], text=True
        ).strip()
        state = request.state or extension_pb2.AgentState()
        state.prompt += f"\n\n<branch>Current branch: {branch}</branch>"
        return extension_pb2.BeforePromptResponse(state=state)

    def BeforeToolCall(self, request, context):
        # Allow all tool calls through
        return extension_pb2.BeforeToolCallResponse(intercept=False)

    def AfterToolCall(self, request, context):
        return extension_pb2.AfterToolCallResponse(result=request.result)

    def ModifySystemPrompt(self, request, context):
        return extension_pb2.ModifySystemPromptResponse(
            modified_prompt=request.current_prompt
        )


def serve():
    # hashicorp/go-plugin negotiates the gRPC port via stdout
    port = os.environ.get("PLUGIN_GRPC_PORT", "1234")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    extension_pb2_grpc.add_ExtensionServicer_to_server(TicketContextServicer(), server)
    server.add_insecure_port(f"127.0.0.1:{port}")
    server.start()
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
```

> **Tip:** Consult the [hashicorp/go-plugin examples](https://github.com/hashicorp/go-plugin/tree/main/examples) for the correct stdout handshake implementation. Debug output must go to stderr to avoid corrupting the protocol.

---

## Extension Lifecycle

```
glm startup
  → Loader scans extension directories
  → Launches each binary/script as a subprocess
  → Performs handshake (GOLLM_EXTENSION cookie check)
  → Establishes gRPC connection
  → Calls Name() and Tools() once

Per prompt turn:
  → ModifySystemPrompt() — extension modifies the system prompt
  → BeforePrompt() — extension can further modify agent state
  → Agent runs, calls tools
  → Per tool call:
      BeforeToolCall() — extension can intercept and short-circuit
      [tool executes if not intercepted]
      AfterToolCall() — extension can observe/modify result

glm shutdown:
  → Loader kills all extension subprocesses
```

---

## Go In-Process Extension (Advanced)

If your extension is written in Go and you control the build, you can implement `agent.Extension` directly and register it without the gRPC overhead. Use `agent.NoopExtension` as a base:

```go
import (
    "context"
    "encoding/json"

    "github.com/goppydae/gollm/internal/agent"
    "github.com/goppydae/gollm/internal/tools"
)

type MyExtension struct {
    agent.NoopExtension
}

func (e *MyExtension) ModifySystemPrompt(prompt string) string {
    return prompt + "\n\nAlways respond in bullet points."
}

func (e *MyExtension) BeforeToolCall(ctx context.Context, call *agent.ToolCall, args json.RawMessage) (*tools.ToolResult, bool) {
    // Block the bash tool entirely
    if call.Name == "bash" {
        return &tools.ToolResult{Content: "bash is disabled", IsError: true}, true
    }
    return nil, false
}
```

The full `agent.Extension` interface:

```go
type Extension interface {
    Name() string
    Tools() []tools.Tool
    BeforePrompt(ctx context.Context, state *agent.AgentState) *agent.AgentState
    BeforeToolCall(ctx context.Context, call *agent.ToolCall, args json.RawMessage) (*tools.ToolResult, bool)
    AfterToolCall(ctx context.Context, call *agent.ToolCall, result *tools.ToolResult) *tools.ToolResult
    ModifySystemPrompt(prompt string) string
}
```

Pass the extension via `ag.SetExtensions()` from the SDK or directly in `cmd/glm`.

---

## Tips

- **Extensions are isolated processes.** A crash in an extension will not crash `gollm` — the loader catches errors and logs them.
- **Keep `BeforePrompt` fast.** It runs before every single LLM call. Avoid blocking network calls; cache data when possible.
- **Use skills for static context.** If you only need to append static text to the system prompt, a skill is simpler than an extension.
- **Extensions are global.** All extensions in the configured directories are loaded for every session. There is no per-project scoping beyond the directory config.
- **Logs go to stderr.** Write debug output to stderr to avoid corrupting the gRPC protocol on stdout.
