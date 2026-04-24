# Tutorial: Creating Extensions

Extensions let you add new behaviors to `gollm` beyond what's possible with skills and prompt templates. They can modify the system prompt before every turn, add custom tools the agent can call, and intercept tool results. Extensions run as separate processes and communicate with `gollm` via gRPC.

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

---

## The Extension Interface (gRPC)

Every extension must implement the `Extension` gRPC service:

```protobuf
service Extension {
  rpc Name(Empty) returns (NameResponse);
  rpc Tools(Empty) returns (ToolsResponse);
  rpc BeforePrompt(BeforePromptRequest) returns (BeforePromptResponse);
  rpc AfterToolCall(AfterToolCallRequest) returns (AfterToolCallResponse);
  rpc ModifySystemPrompt(ModifySystemPromptRequest) returns (ModifySystemPromptResponse);
}
```

| RPC | When called | Purpose |
|---|---|---|
| `Name` | On load | Returns the extension's identifier string |
| `Tools` | On load | Returns tool definitions the agent can call |
| `BeforePrompt` | Before each LLM call | Modify agent state (e.g. append to system prompt) |
| `AfterToolCall` | After each tool execution | Observe or modify tool results |
| `ModifySystemPrompt` | On load / config change | Initial system prompt modification |

You only need to implement the RPCs relevant to your extension. Unimplemented RPCs can return empty responses.

---

## The Handshake

Extensions must implement the [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) handshake:

```
MAGIC_COOKIE_KEY:   GOLLM_EXTENSION
MAGIC_COOKIE_VALUE: v1.0.0
Protocol version:   1
```

The extension process reads these values from environment variables set by the loader and responds via gRPC on a negotiated port.

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
    "fmt"
    "os/exec"
    "strings"

    "github.com/hashicorp/go-plugin"
    "google.golang.org/grpc"

    // Import the glm extension proto from your vendored copy or replace
    // with the generated types from gollm/extensions/proto
    proto "github.com/goppydae/gollm/extensions/proto"
)

// GitContextExtension injects the current git branch and recent commits
// into the system prompt before each turn.
type GitContextExtension struct {
    proto.UnimplementedExtensionServer
}

func (e *GitContextExtension) Name(ctx context.Context, _ *proto.Empty) (*proto.NameResponse, error) {
    return &proto.NameResponse{Name: "git-context"}, nil
}

func (e *GitContextExtension) Tools(ctx context.Context, _ *proto.Empty) (*proto.ToolsResponse, error) {
    return &proto.ToolsResponse{}, nil
}

func (e *GitContextExtension) BeforePrompt(ctx context.Context, req *proto.BeforePromptRequest) (*proto.BeforePromptResponse, error) {
    branch := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
    status := gitOutput("status", "--short")
    log := gitOutput("log", "--oneline", "-5")

    addition := fmt.Sprintf(
        "\n\n<git_context>\nBranch: %s\n\nRecent commits:\n%s\n\nWorking tree status:\n%s\n</git_context>",
        branch, log, status,
    )

    newState := req.State
    if newState == nil {
        newState = &proto.AgentState{}
    }
    newState.Prompt += addition

    return &proto.BeforePromptResponse{State: newState}, nil
}

func (e *GitContextExtension) AfterToolCall(ctx context.Context, req *proto.AfterToolCallRequest) (*proto.AfterToolCallResponse, error) {
    return &proto.AfterToolCallResponse{Result: req.Result}, nil
}

func (e *GitContextExtension) ModifySystemPrompt(ctx context.Context, req *proto.ModifySystemPromptRequest) (*proto.ModifySystemPromptResponse, error) {
    return &proto.ModifySystemPromptResponse{ModifiedPrompt: req.CurrentPrompt}, nil
}

func gitOutput(args ...string) string {
    out, err := exec.Command("git", args...).Output()
    if err != nil {
        return "(unavailable)"
    }
    return strings.TrimSpace(string(out))
}

// Plugin wrapper for hashicorp/go-plugin
type ExtensionPlugin struct {
    Impl *GitContextExtension
}

func (p *ExtensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
    proto.RegisterExtensionServer(s, p.Impl)
    return nil
}

func (p *ExtensionPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
    return nil, nil
}

func main() {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: plugin.HandshakeConfig{
            ProtocolVersion:  1,
            MagicCookieKey:   "GOLLM_EXTENSION",
            MagicCookieValue: "v1.0.0",
        },
        Plugins: map[string]plugin.Plugin{
            "extension": &ExtensionPlugin{Impl: &GitContextExtension{}},
        },
        GRPCServer: plugin.DefaultGRPCServer,
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

## Example: Python Extension

Python extensions work the same way but are invoked with the Python interpreter. You'll need the `grpc` and `go-plugin` Python libraries.

> **Note:** Python extension support requires `grpcio` and a compatible proto stub. The proto file is at `extensions/proto/extension.proto` in the glm repository.

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
        # Example: inject the current JIRA ticket from the branch name
        branch = subprocess.check_output(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            text=True
        ).strip()

        # Extract ticket ID from branch name (e.g. "feature/PROJ-123-my-feature")
        ticket = ""
        parts = branch.split("/")
        if len(parts) > 1:
            segment = parts[-1].upper()
            for part in segment.split("-"):
                if part.isdigit() and ticket:
                    ticket = ticket + "-" + part
                    break
                elif part.isalpha():
                    ticket = part

        state = request.state or extension_pb2.AgentState()
        if ticket:
            state.prompt += f"\n\n<ticket>Current work is related to ticket: {ticket}</ticket>"

        return extension_pb2.BeforePromptResponse(state=state)

    def AfterToolCall(self, request, context):
        return extension_pb2.AfterToolCallResponse(result=request.result)

    def ModifySystemPrompt(self, request, context):
        return extension_pb2.ModifySystemPromptResponse(
            modified_prompt=request.current_prompt
        )


def serve():
    # hashicorp/go-plugin protocol: read port from env
    port = os.environ.get("PLUGIN_GRPC_PORT", "1234")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    extension_pb2_grpc.add_ExtensionServicer_to_server(TicketContextServicer(), server)
    server.add_insecure_port(f"127.0.0.1:{port}")
    server.start()
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
```

> **Tip:** For a working Python extension, consult the [hashicorp/go-plugin Python helper](https://github.com/hashicorp/go-plugin/tree/main/examples) for the correct handshake implementation. The protocol negotiates the gRPC port via stdout.

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
  → Calls BeforePrompt() — extension can modify system prompt
  → Agent runs, calls tools
  → Per tool call: AfterToolCall() — extension can observe/modify result

glm shutdown:
  → Loader kills all extension subprocesses
```

---

## Go In-Process Extension (Advanced)

If your extension is written in Go and you control the `gollm` build, you can implement the `agent.Extension` interface directly and register it without the gRPC overhead:

```go
// internal/agent/extension.go interface:
type Extension interface {
    Name() string
    Tools() []tools.Tool
    BeforePrompt(ctx context.Context, state *AgentState) *AgentState
}
```

Use the `NoopExtension` embed for methods you don't need:

```go
type MyExtension struct {
    agent.NoopExtension
}

func (e *MyExtension) BeforePrompt(ctx context.Context, state *agent.AgentState) *agent.AgentState {
    newState := *state
    newState.SystemPrompt += "\n\nAlways respond in bullet points."
    return &newState
}
```

Pass it to `ag.SetExtensions()` via the SDK or directly from `cmd/gollm`.

---

## Tips

- **Extensions are isolated processes.** A crash in an extension will not crash `gollm` — the loader catches errors and logs them.
- **Keep `BeforePrompt` fast.** It runs before every single LLM call. Avoid blocking network calls; cache data when possible.
- **Use skills for static context.** If you only need to append static text to the system prompt, a skill is simpler than an extension.
- **Extensions are global.** All extensions in the configured directories are loaded for every session. There is no per-project scoping beyond the directory config.
- **Logs go to stderr.** Write debug output to stderr to avoid corrupting the gRPC protocol on stdout.
