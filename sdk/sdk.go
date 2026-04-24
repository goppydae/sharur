// Package sdk provides the public Go SDK for embedding gollm agents in your own applications.
//
// Example:
//
//	agent, err := sdk.NewAgent(sdk.Config{
//	    Model:    "llama3",
//	    Provider: "ollama",
//	    Tools:    []sdk.Tool{tools.Read{}, tools.Bash{}},
//	})
//	if err != nil { panic(err) }
//
//	agent.Subscribe(func(e sdk.Event) {
//	    if e.Type == sdk.EventTextDelta {
//	        fmt.Print(e.Content)
//	    }
//	})
//
//	agent.Prompt(context.Background(), "What files are in this directory?")
//	<-agent.Idle()
package sdk

import (
	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// Re-export core types so consumers import only this package.

// Agent is the stateful conversation agent.
type Agent = agent.Agent

// Event is an agent lifecycle event emitted to subscribers.
type Event = agent.Event

// EventType identifies the kind of event.
type EventType = agent.EventType

// Tool is the universal tool interface.
type Tool = tools.Tool

// ToolResult is the output of a tool execution.
type ToolResult = tools.ToolResult

// ThinkingLevel controls how much reasoning budget the model gets.
type ThinkingLevel = types.ThinkingLevel

const (
	ThinkingOff    ThinkingLevel = types.ThinkingOff
	ThinkingLow    ThinkingLevel = types.ThinkingLow
	ThinkingMedium ThinkingLevel = types.ThinkingMedium
	ThinkingHigh   ThinkingLevel = types.ThinkingHigh
)

const (
	EventAgentStart   = agent.EventAgentStart
	EventTurnStart    = agent.EventTurnStart
	EventMessageStart = agent.EventMessageStart
	EventTextDelta    = agent.EventTextDelta
	EventToolCall     = agent.EventToolCall
	EventMessageEnd   = agent.EventMessageEnd
	EventAgentEnd     = agent.EventAgentEnd
	EventError        = agent.EventError
	EventAbort        = agent.EventAbort
)

// Config holds the options for creating a new agent.
type Config struct {
	// Provider selects the LLM backend: "ollama" (default), "openai", or "anthropic".
	Provider string

	// Model is the model name to use (e.g. "llama3", "gpt-4o", "claude-sonnet-4-6").
	Model string

	// OllamaURL overrides the Ollama base URL (default: http://localhost:11434).
	OllamaURL string

	// OpenAIURL overrides the OpenAI-compatible base URL.
	OpenAIURL string

	// OpenAIKey is the API key for OpenAI or any compatible provider.
	OpenAIKey string

	// AnthropicKey is the Anthropic API key.
	AnthropicKey string

	// SystemPrompt sets the agent's system prompt.
	SystemPrompt string

	// ThinkingLevel controls reasoning depth.
	ThinkingLevel ThinkingLevel

	// MaxTokens caps the response length (0 = provider default).
	MaxTokens int

	// Tools registers additional tools beyond the builtins.
	// Pass tools.Read{}, tools.Write{}, tools.Bash{}, etc.
	Tools []Tool

	// Extensions registers active extensions (gRPC plugins or Skills).
	Extensions []agent.Extension
}

// NewAgent creates a new agent from the given configuration.
func NewAgent(cfg Config) (*Agent, error) {
	var provider llm.Provider
	switch cfg.Provider {
	case "openai":
		provider = llm.NewOpenAIProviderWithKey(cfg.OpenAIURL, cfg.Model, cfg.OpenAIKey)
	case "anthropic":
		provider = llm.NewAnthropicProvider(cfg.AnthropicKey, cfg.Model)
	default:
		base := cfg.OllamaURL
		if base == "" {
			base = "http://localhost:11434"
		}
		provider = llm.NewOllamaProvider(base, cfg.Model)
	}

	registry := tools.NewToolRegistry()
	for _, t := range cfg.Tools {
		registry.Register(t)
	}

	ag := agent.New(provider, registry)

	if cfg.SystemPrompt != "" {
		ag.SetSystemPrompt(cfg.SystemPrompt)
	}
	if cfg.ThinkingLevel != "" {
		ag.SetThinkingLevel(cfg.ThinkingLevel)
	}
	if cfg.MaxTokens > 0 {
		ag.SetMaxTokens(cfg.MaxTokens)
	}
	if len(cfg.Extensions) > 0 {
		ag.SetExtensions(cfg.Extensions)
	}

	return ag, nil
}

// DefaultTools returns the full built-in tool set.
func DefaultTools() []Tool {
	return []Tool{
		tools.Read{},
		tools.Write{},
		tools.Edit{},
		tools.Bash{},
		tools.Grep{},
		tools.Find{},
		tools.Ls{},
	}
}
