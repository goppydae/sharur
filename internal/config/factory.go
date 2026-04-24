package config

import (
	"fmt"
	"strings"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
)

// BuildProvider creates an llm.Provider based on the configuration.
// It returns an error if a required API key for the chosen provider is missing.
func BuildProvider(cfg *Config) (llm.Provider, error) {
	switch cfg.Provider {
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("provider %q requires openAIApiKey in config or OPENAI_API_KEY env var", cfg.Provider)
		}
		return llm.NewOpenAIProviderWithKey(cfg.OpenAIBaseURL, cfg.Model, cfg.OpenAIAPIKey), nil
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("provider %q requires anthropicApiKey in config or ANTHROPIC_API_KEY env var", cfg.Provider)
		}
		return llm.NewAnthropicProvider(cfg.AnthropicAPIKey, cfg.Model), nil
	case "google":
		if cfg.GoogleAPIKey == "" {
			return nil, fmt.Errorf("provider %q requires googleApiKey in config or GOOGLE_API_KEY env var", cfg.Provider)
		}
		return llm.NewGoogleProvider(cfg.GoogleAPIKey, cfg.Model), nil
	case "llamacpp":
		return llm.NewLlamaCppProvider(cfg.LlamaCppBaseURL), nil
	default:
		return llm.NewOllamaProvider(cfg.OllamaBaseURL, cfg.Model), nil
	}
}

// BuildToolRegistry builds a tool registry respecting DisabledTools / EnabledTools config.
//
// Precedence rules (highest to lowest):
//  1. DisabledTools=true  → empty registry, all tools disabled regardless of EnabledTools.
//  2. EnabledTools=[...]  → only the named tools are registered.
//  3. Neither set        → all built-in tools are registered.
func BuildToolRegistry(cfg *Config) *tools.ToolRegistry {
	if cfg.DisabledTools {
		return tools.NewToolRegistry()
	}

	allTools := []tools.Tool{
		tools.Read{},
		tools.Write{},
		tools.Edit{},
		tools.Bash{},
		tools.Grep{},
		tools.Find{},
		tools.Ls{},
	}

	if len(cfg.EnabledTools) > 0 {
		enabled := make(map[string]bool)
		for _, t := range cfg.EnabledTools {
			enabled[strings.TrimSpace(t)] = true
		}
		r := tools.NewToolRegistry()
		for _, t := range allTools {
			if enabled[t.Name()] {
				r.Register(t)
			}
		}
		return r
	}

	r := tools.NewToolRegistry()
	for _, t := range allTools {
		r.Register(t)
	}
	return r
}
