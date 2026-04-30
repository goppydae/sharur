---
title: TUI
weight: 30
description: Keybindings, slash commands, bang commands, and at-file attachments
---

The TUI is a rich, Bubble Tea-powered interface with real-time streaming, tool cards, session management, and a live context usage progress bar in the status footer.

---

## Keybindings

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

---

## Slash Commands

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
| `/model <p/m>` | Switch model mid-conversation (e.g. `/model anthropic/claude-sonnet-4-6`) |
| `/stats` | View session statistics and token usage |
| `/config` | View and edit active configuration |
| `/context` | View detailed context window usage |
| `/compact` | Manually trigger a context compaction |
| `/skill:<name> [args]` | Invoke a skill |
| `/prompt:<name>` | Expand a prompt template into the editor |
| `/exit` | Quit (alias: `/quit`) |

---

## Session Tree Modal (`/tree`)

| Key | Action |
|---|---|
| `↑/↓` / `PgUp/PgDn` | Navigate the session list |
| `Enter` | Resume the selected session (or branch from it if it's an interior node) |
| `B` | Create a new **branch** from the selected session |
| `F` | Create an independent **fork** of the selected session |
| `R` | Start an interactive **rebase** from the selected session's history |
| `Esc` | Close modal |

---

## Bang Commands

Bang commands execute a shell command and inject the output into the conversation:

```bash
!ls -la          # Execute shell command, paste output into editor
!!cat README.md  # Execute shell command, send output directly to agent
```

- `!cmd` — pastes stdout into the editor so you can review before sending
- `!!cmd` — sends stdout directly to the agent without review

---

## At-File Attachments

Type `@` in the input to fuzzy-search and attach file contents to your prompt:

```
Tell me what this does @src/agent/loop.go
```

The file content is embedded inline in the message sent to the agent.
