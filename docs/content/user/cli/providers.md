---
title: Provider Setup
weight: 20
description: Configuring each LLM provider — API keys, base URLs, model names, and env vars
categories: [cli, configuration]
tags: [providers]
---

`sharur` supports five LLM providers. All configuration lives in `config.json` files or environment variables; environment variables take priority over config file values.

---

## Model Naming

Models can be specified as `provider/model` shorthand or with separate flags:

```bash
# Shorthand: provider inferred from the slash-prefix
shr --model anthropic/claude-sonnet-4-6

# Explicit: provider and model as separate flags
shr --provider anthropic --model claude-sonnet-4-6
```

Both forms are equivalent. The shorthand is convenient for one-off overrides; the config file form is better for persistent defaults.

---

## Environment Variables

API keys set via environment variable take priority over values in `config.json`. The env var names use the `SHARUR_` prefix:

| Provider | Environment Variable |
|---|---|
| Anthropic | `SHARUR_ANTHROPIC_API_KEY` |
| OpenAI | `SHARUR_OPENAI_API_KEY` |
| Google | `SHARUR_GOOGLE_API_KEY` |

Ollama and llama.cpp are local servers and do not use API keys.

---

## Ollama

[Ollama](https://ollama.com) runs models locally. It is the default provider.

```jsonc
// ~/.sharur/config.json or .sharur/config.json
{
  "defaultProvider": "ollama",
  "defaultModel": "llama3.2",
  "ollamaBaseURL": "http://localhost:11434"
}
```

```bash
# Pull a model and launch
ollama pull llama3.2
shr

# Use a specific model
shr --model ollama/llama3.2

# Point at a remote Ollama server
shr --model llama3.2 --provider ollama
```

**Notes:**
- Default base URL is `http://localhost:11434`. Override with `ollamaBaseURL`.
- Ollama models support tools and images (vision models).
- Use `shr --list-models` to see all locally available models.
- Thinking is supported on models that emit `<think>` tokens (e.g. `qwq`, `deepseek-r1`).

---

## llama.cpp

[llama.cpp](https://github.com/ggerganov/llama.cpp) exposes an OpenAI-compatible HTTP server.

```jsonc
{
  "defaultProvider": "llamacpp",
  "llamaCppBaseURL": "http://localhost:8080"
}
```

```bash
# Start the llama.cpp server (example)
./llama-server -m model.gguf --port 8080

# Connect with sharur
shr --provider llamacpp --model my-model
```

**Notes:**
- Default base URL is `http://localhost:8080`. Override with `llamaCppBaseURL`.
- The model name passed to `shr` is forwarded to the server as-is.
- Image attachments are not supported.
- The server's own context window size is used; `sharur` queries `/v1/models` to detect it.

---

## OpenAI

```jsonc
{
  "defaultProvider": "openai",
  "defaultModel": "gpt-4o",
  "openAIApiKey": "",
  "openAIBaseURL": "https://api.openai.com/v1"
}
```

```bash
# Via environment variable (recommended)
export SHARUR_OPENAI_API_KEY=sk-...
shr --model openai/gpt-4o

# One-off key override
shr --provider openai --model gpt-4o --api-key sk-...
```

**OpenAI-compatible endpoints:**

Any server that implements the OpenAI chat completions API can be used by pointing `openAIBaseURL` at it:

```jsonc
{
  "defaultProvider": "openai",
  "openAIBaseURL": "http://localhost:11434/v1",
  "openAIApiKey": "unused"
}
```

This works with [vLLM](https://github.com/vllm-project/vllm), [LM Studio](https://lmstudio.ai), and others.

**Notes:**
- Reasoning models (o3, o4-mini) emit thinking deltas that appear in the TUI and JSON event stream.
- Supports tools and vision (images) for compatible models.

---

## Anthropic

```jsonc
{
  "defaultProvider": "anthropic",
  "defaultModel": "claude-sonnet-4-6",
  "anthropicApiKey": "",
  "anthropicApiVersion": ""
}
```

```bash
export SHARUR_ANTHROPIC_API_KEY=sk-ant-...
shr --model anthropic/claude-sonnet-4-6

# Extended thinking (claude-3-7-sonnet and later)
shr --model anthropic/claude-3-7-sonnet-20250219 --thinking high
```

**Notes:**
- Extended thinking is supported for models that enable it (e.g. `claude-3-7-sonnet`). Use `--thinking medium` or `--thinking high`.
- `medium` thinking uses a 10,000-token budget; `high` uses 20,000 tokens. Temperature is automatically set to the required value.
- `anthropicApiVersion` overrides the `anthropic-version` request header; leave empty to use the library default.

---

## Google Gemini

```jsonc
{
  "defaultProvider": "google",
  "defaultModel": "gemini-2.0-flash",
  "googleApiKey": ""
}
```

```bash
export SHARUR_GOOGLE_API_KEY=AIza...
shr --model google/gemini-2.0-flash
```

**Notes:**
- Gemini 1.5 Pro and later have a 1M+ token context window.
- Supports tools and vision (images).
- Use `shr --list-models` to see available Gemini models.

---

## Listing Available Models

All five providers implement model listing. Use `--list-models` to query the active provider:

```bash
# List Ollama models
shr --list-models

# List models from a specific provider
shr --provider anthropic --list-models

# Filter results
shr --provider openai --list-models gpt-4
```

The output is a plain list of model names, suitable for piping:

```bash
shr --list-models | fzf | xargs -I{} shr --model {}
```

---

## Provider Feature Matrix

| Provider | Tools | Images | Thinking | Model Listing |
|---|:---:|:---:|:---:|:---:|
| `ollama` | ✓ | ✓ | model-dependent | ✓ |
| `llamacpp` | ✓ | ✗ | ✗ | ✓ |
| `openai` | ✓ | ✓ | reasoning models | ✓ |
| `anthropic` | ✓ | ✓ | ✓ extended | ✓ |
| `google` | ✓ | ✓ | ✗ | ✓ |
