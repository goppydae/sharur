package config

import (
	"strings"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
)

// BuildProvider creates an llm.Provider based on the configuration.
func BuildProvider(cfg *Config) llm.Provider {
	switch cfg.Provider {
	case "openai":
		return llm.NewOpenAIProviderWithKey(cfg.OpenAIBaseURL, cfg.Model, cfg.OpenAIAPIKey)
	case "anthropic":
		return llm.NewAnthropicProvider(cfg.AnthropicAPIKey, cfg.Model)
	case "llamacpp":
		return llm.NewLlamaCppProvider(cfg.LlamaCppBaseURL)
	case "google":
		return llm.NewGoogleProvider(cfg.GoogleAPIKey, cfg.Model)
	default:
		return llm.NewOllamaProvider(cfg.OllamaBaseURL, cfg.Model)
	}
}

// BuildToolRegistry builds a tool registry respecting DisabledTools / EnabledTools config.
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
