package config

import (
	"strings"
	"testing"
)

// TestBuildProvider_APIKeyValidation verifies that providers requiring API keys
// return an actionable error at startup rather than failing silently on the first request.
func TestBuildProvider_APIKeyValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		wantErr     bool
		errContains string
	}{
		{
			name:        "anthropic missing key",
			cfg:         &Config{Provider: "anthropic", Model: "claude-sonnet-4-6"},
			wantErr:     true,
			errContains: "anthropicApiKey",
		},
		{
			name:        "openai missing key",
			cfg:         &Config{Provider: "openai", Model: "gpt-4o"},
			wantErr:     true,
			errContains: "openAIApiKey",
		},
		{
			name:        "google missing key",
			cfg:         &Config{Provider: "google", Model: "gemini-pro"},
			wantErr:     true,
			errContains: "googleApiKey",
		},
		{
			name:    "anthropic with key",
			cfg:     &Config{Provider: "anthropic", Model: "claude-sonnet-4-6", AnthropicAPIKey: "sk-test"},
			wantErr: false,
		},
		{
			name:    "openai with key",
			cfg:     &Config{Provider: "openai", Model: "gpt-4o", OpenAIAPIKey: "sk-test"},
			wantErr: false,
		},
		{
			name:    "google with key",
			cfg:     &Config{Provider: "google", Model: "gemini-pro", GoogleAPIKey: "AIza-test"},
			wantErr: false,
		},
		{
			name:    "llamacpp needs no key",
			cfg:     &Config{Provider: "llamacpp", LlamaCppBaseURL: "http://localhost:8080"},
			wantErr: false,
		},
		{
			name:    "ollama (default) needs no key",
			cfg:     &Config{Provider: "ollama", OllamaBaseURL: "http://localhost:11434"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildProvider(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not mention %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestBuildToolRegistry_Precedence verifies DisabledTools takes priority over EnabledTools.
func TestBuildToolRegistry_Precedence(t *testing.T) {
	t.Run("DisabledTools empties registry", func(t *testing.T) {
		cfg := &Config{DisabledTools: true, EnabledTools: []string{"bash", "read"}}
		r := BuildToolRegistry(cfg)
		if len(r.All()) != 0 {
			t.Errorf("expected empty registry when DisabledTools=true, got %d tools", len(r.All()))
		}
	})

	t.Run("EnabledTools filters to named subset", func(t *testing.T) {
		cfg := &Config{EnabledTools: []string{"bash", "read"}}
		r := BuildToolRegistry(cfg)
		if !r.Has("bash") {
			t.Error("expected 'bash' in registry")
		}
		if !r.Has("read") {
			t.Error("expected 'read' in registry")
		}
		if r.Has("write") {
			t.Error("'write' should not be in registry when not in EnabledTools")
		}
	})

	t.Run("no flags registers all built-in tools", func(t *testing.T) {
		cfg := &Config{}
		r := BuildToolRegistry(cfg)
		for _, name := range []string{"bash", "read", "write", "edit", "grep", "find", "ls"} {
			if !r.Has(name) {
				t.Errorf("expected %q in default registry", name)
			}
		}
	})
}
