package themes

import (
	"path/filepath"
	"testing"
)

func TestParseAdaptiveColor(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"#fff", "#ffffff", false},
		{"#000000", "#000000", false},
		{"#ff00ffaa", "#ff00ff", false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := ParseAdaptiveColor(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseAdaptiveColor(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got.Light != tt.expected {
			t.Errorf("ParseAdaptiveColor(%q) = %q, want %q", tt.input, got.Light, tt.expected)
		}
	}
}

func TestLoadSaveTheme(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test-theme.json")
	
	orig := &Theme{
		Name: "test-theme",
		Accent: AdaptiveColor{Light: "#ff0000", Dark: "#00ff00"},
	}

	if err := SaveTheme(path, orig); err != nil {
		t.Fatalf("SaveTheme() error: %v", err)
	}

	loaded, err := LoadTheme(path)
	if err != nil {
		t.Fatalf("LoadTheme() error: %v", err)
	}

	if loaded.Name != orig.Name {
		t.Errorf("expected name %s, got %s", orig.Name, loaded.Name)
	}
	if loaded.Accent.Light != orig.Accent.Light {
		t.Errorf("expected light accent %s, got %s", orig.Accent.Light, loaded.Accent.Light)
	}
}

func TestNewStyle(t *testing.T) {
	theme := Theme{
		Name:   "test",
		Accent: AdaptiveColor{Light: "#ff0000", Dark: "#00ff00"},
		Bordered: AdaptiveColor{Light: "#cccccc", Dark: "#333333"},
		Muted: AdaptiveColor{Light: "#888888", Dark: "#777777"},
		Dim: AdaptiveColor{Light: "#444444", Dark: "#222222"},
	}
	style := NewStyle(theme)
	
	// Exercise style methods
	_ = style.Header()
	_ = style.Logo()
	_ = style.Hint()
	_ = style.Footer()
	_ = style.BorderTop()
	_ = style.UserBox()
	_ = style.AssistantBox()
	_ = style.NoticeBox("error")
	_ = style.NoticeBox("warning")
	_ = style.NoticeBox("success")
	_ = style.NoticeBox("info")
	_ = style.WorkingIndicator()
	_ = style.StatusIdle()
	_ = style.StatusWorking()
	_ = style.Dim()
	_ = style.Muted()
	_ = style.UserMessage()
	_ = style.AssistantMessage()
	_ = style.NoticeMsg("error")
	_ = style.ThinkingBox()
	_ = style.ToolCall()
	
	// Exercise color accessors
	_ = style.AccentColor()
	_ = style.AccentTextColor()
	_ = style.MutedColor()
	_ = style.MutedTextColor()
	_ = style.SuccessColor()
	_ = style.ErrorColor()
	_ = style.WarningColor()
	_ = style.InfoColor()
	_ = style.WorkingColor()
	_ = style.PanelBgColor()
	_ = style.ToolRunningBgColor()
	_ = style.ToolSuccessBgColor()
	_ = style.ToolFailureBgColor()
	_ = style.Bordered()
	_ = style.FooterPaddingX()
}
