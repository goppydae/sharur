---
title: gRPC Mode
weight: 50
description: Persistent multi-session gRPC service
---

gRPC mode starts a persistent `AgentService` server. Each connecting client supplies a `session_id` and gets its own isolated agent. Sessions are saved to disk after each turn and reloaded automatically on reconnect.

```bash
# Start on the default port
glm --mode grpc

# Use a custom address
glm --mode grpc --grpc-addr :9090
```

The server responds to `SIGINT`/`SIGTERM` with a graceful shutdown: in-flight turns are allowed to finish (30 s timeout), all sessions are flushed to disk, then the listener closes.

---

## Proto Definition

The service is defined in `proto/gollm/v1/agent.proto`. Generated Go stubs live in `internal/gen/gollm/v1/`. Regenerate with `mage generate`.

Key RPCs:

| RPC | Description |
|---|---|
| `Prompt` | Send a user message; streams back `AgentEvent`s |
| `NewSession` | Create a new session |
| `GetMessages` | Retrieve message history for a session |
| `GetState` | Get current agent state |
| `Steer` | Inject a steering message mid-turn |
| `FollowUp` | Queue a follow-up after the current turn |
| `Abort` | Cancel the current running turn |
| `ForkSession` | Fork a session into a new independent copy |
| `ConfigureSession` | Change model, provider, or thinking level |

---

## In-Process Transport

For the TUI and JSON modes, all internal communication also goes through this same protobuf boundary using a `bufconn` in-memory pipe — not a network socket. This means all three modes share identical code paths. See [Service Architecture](/developer/internals/service/) for details.
