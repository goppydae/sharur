---
title: Configuration
weight: 10
description: config.json schema, layering, and CLI flag reference
---

`gollm` uses layered JSON configuration. Project-level settings override global defaults.

| Path | Scope |
|---|---|
| `~/.gollm/config.json` | Global defaults — applies to all projects |
| `.gollm/config.json` | Project-level overrides — applies in this directory |

---

## config.json Schema

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

API keys can also be set via environment variables — env vars take priority over config file values.

---

## Context Files

`gollm` auto-discovers `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, and `.context.md` in your project root and parent directories and injects them into the system prompt. Outermost files take precedence (parent directory wins over project root).

Disable with `--no-context-files`.

---

## CLI Flags

### Mode

| Flag | Description |
|---|---|
| `--mode` | Mode: `tui` (default), `json`, `grpc` |
| `--grpc-addr` | gRPC listen address (default `:50051`; `--mode grpc` only) |

### Model / Provider

| Flag | Description |
|---|---|
| `--model` / `-m` | Model to use (e.g. `llama3`, `gpt-4o`, `anthropic/claude-sonnet-4-6`) |
| `--provider` | Provider: `ollama`, `openai`, `anthropic`, `llamacpp`, `google` |
| `--api-key` | API key override |
| `--thinking` | Thinking level: `off`, `minimal`, `low`, `medium`, `high`, `xhigh` |
| `--models` | Comma-separated model list for `Ctrl+P` cycling |

### Session

| Flag | Description |
|---|---|
| `--continue` / `-c` | Resume the most recent session |
| `--resume` / `-r` | Select a session to resume (fuzzy search or ID) |
| `--session` | Use a specific session file path |
| `--session-dir` | Directory for session storage and lookup |
| `--branch` | Branch from a session file or partial UUID into a new child session |
| `--no-session` | Ephemeral mode: don't save the session |

### System Prompt

| Flag | Description |
|---|---|
| `--system-prompt` | Override the system prompt |
| `--append-system-prompt` | Append text or file to the system prompt (repeatable) |

### Tools

| Flag | Description |
|---|---|
| `--tools` | Comma-separated list of tools to enable: `read,bash,edit,write,grep,find,ls` |
| `--no-tools` | Disable all built-in tools |
| `--dry-run` | Safety mode: destructive tools preview actions instead of running |

### Extensions / Skills / Prompts

| Flag | Description |
|---|---|
| `--extension` / `-e` | Load a gRPC extension binary (repeatable) |
| `--no-extensions` | Disable extension directory auto-discovery (`-e` paths still load) |
| `--skill` | Load a skill file or directory (repeatable) |
| `--no-skills` | Disable skill auto-discovery |
| `--prompt-template` | Load a prompt template file or directory (repeatable) |
| `--no-prompt-templates` | Disable prompt template auto-discovery |

### Output / Info

| Flag | Description |
|---|---|
| `--export` | Export current session to an HTML file and exit |
| `--list-models` | List available models from the configured provider (optional fuzzy filter) |
| `--version` / `-v` | Show version number |
| `--verbose` | Force verbose startup output |
| `--offline` | Disable startup network operations (model checks, etc.) |
