package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Model != "local" {
		t.Errorf("expected model local, got %s", cfg.Model)
	}
	if cfg.ThinkingLevel != "medium" {
		t.Errorf("expected thinkingLevel medium, got %s", cfg.ThinkingLevel)
	}
}

func TestConfig_Validate(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected default config to be valid, got %v", err)
	}

	cfg.ThinkingLevel = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid thinkingLevel, got nil")
	}

	cfg.ThinkingLevel = "high"
	cfg.Compaction.ReserveTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative reserveTokens, got nil")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	
	cases := []struct {
		input    string
		expected string
	}{
		{"/abs/path", "/abs/path"},
		{"~/rel/path", filepath.Join(home, "rel/path")},
		{"~", home},
	}

	for _, c := range cases {
		got := expandPath(c.input)
		if got != c.expected {
			t.Errorf("expandPath(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestLoad(t *testing.T) {
	// Create a temporary directory for testing config files
	tmpDir := t.TempDir()
	
	// Create a mock home directory
	mockHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(filepath.Join(mockHome, ".gollm"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write a global config
	globalConfigPath := filepath.Join(mockHome, ".gollm", "config.json")
	globalConfig := `{"defaultModel": "global-model", "defaultProvider": "openai"}`
	if err := os.WriteFile(globalConfigPath, []byte(globalConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Set HOME env var so Load finds it
	origHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", mockHome); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Setenv("HOME", origHome); err != nil {
			t.Logf("failed to restore HOME: %v", err)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Model != "global-model" {
		t.Errorf("expected model global-model, got %s", cfg.Model)
	}
}
