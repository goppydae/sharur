// Package themes provides a token-based color system for the gollm TUI.
// Themes are loaded from JSON/YAML config files or used from the bundled set.
package themes

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"gopkg.in/yaml.v3"
)

// DefaultDirs returns the standard theme search directories.
func DefaultDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".gollm", "themes"),
		".gollm/themes",
	}
}

// Theme defines semantic color tokens for the TUI.
type Theme struct {
	Name string `json:"name" yaml:"name"`

	// Core UI
	Accent   AdaptiveColor `json:"accent" yaml:"accent"`
	Bordered AdaptiveColor `json:"bordered" yaml:"bordered"`
	Muted    AdaptiveColor `json:"muted" yaml:"muted"`
	Dim      AdaptiveColor `json:"dim" yaml:"dim"`

	// Feedback
	Success AdaptiveColor `json:"success" yaml:"success"`
	Error   AdaptiveColor `json:"error" yaml:"error"`
	Warning AdaptiveColor `json:"warning" yaml:"warning"`
	Info    AdaptiveColor `json:"info" yaml:"info"`

	// Content
	AccentText   AdaptiveColor `json:"accentText" yaml:"accentText"`
	MutedText    AdaptiveColor `json:"mutedText" yaml:"mutedText"`
	DimText      AdaptiveColor `json:"dimText" yaml:"dimText"`
	WorkingColor AdaptiveColor `json:"workingColor" yaml:"workingColor"`
	ThinkingText AdaptiveColor `json:"thinkingText" yaml:"thinkingText"`

	// Message backgrounds
	UserMsgBg    AdaptiveColor `json:"userMsgBg" yaml:"userMsgBg"`
	AssistantBg  AdaptiveColor `json:"assistantBg" yaml:"assistantBg"`
	ErrorBg       AdaptiveColor `json:"errorBg" yaml:"errorBg"`
	WarningBg     AdaptiveColor `json:"warningBg" yaml:"warningBg"`
	InfoBg        AdaptiveColor `json:"infoBg" yaml:"infoBg"`
	SuccessBg     AdaptiveColor `json:"successBg" yaml:"successBg"`
	ToolRunningBg AdaptiveColor `json:"toolRunningBg" yaml:"toolRunningBg"`
	ToolSuccessBg AdaptiveColor `json:"toolSuccessBg" yaml:"toolSuccessBg"`
	ToolFailureBg AdaptiveColor `json:"toolFailureBg" yaml:"toolFailureBg"`

	// Layout
	HeaderMarginTop int `json:"headerMarginTop,omitempty" yaml:"headerMarginTop,omitempty"`
	FooterPaddingX  int `json:"footerPaddingX,omitempty" yaml:"footerPaddingX,omitempty"`
	MessageMargin   int `json:"messageMargin,omitempty" yaml:"messageMargin,omitempty"`
	ChatPaddingX    int `json:"chatPaddingX,omitempty" yaml:"chatPaddingX,omitempty"`
}

// AdaptiveColor holds light/dark mode colors as hex strings.
// It implements json.Marshaler/Unmarshaler and yaml.Marshaler/Unmarshaler.
type AdaptiveColor struct {
	Light string `json:"light" yaml:"light"`
	Dark  string `json:"dark" yaml:"dark"`
}

// Color returns the appropriate color.Color for the current environment.
func (c AdaptiveColor) Color() color.Color {
	return compat.AdaptiveColor{Light: lipgloss.Color(c.Light), Dark: lipgloss.Color(c.Dark)}
}

// Style wraps a theme and provides ready-to-use lipgloss styles.
type Style struct {
	theme Theme
}

// NewStyle creates a style from the given theme.
func NewStyle(t Theme) Style {
	return Style{theme: t}
}

func (s Style) Header() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.Accent.Color()).
		Bold(true).
		MarginTop(s.theme.HeaderMarginTop).
		PaddingLeft(2)
}

func (s Style) Logo() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.Accent.Color()).
		Bold(true)
}

func (s Style) Hint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.theme.Dim.Color())
}

func (s Style) Footer() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.Muted.Color()).
		PaddingLeft(s.theme.FooterPaddingX)
}

func (s Style) BorderTop() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(s.theme.Bordered.Color())
}

func (s Style) UserBox() lipgloss.Style {
	style := lipgloss.NewStyle().
		Background(s.theme.UserMsgBg.Color()).
		PaddingLeft(2).PaddingRight(2).
		MarginBottom(s.theme.MessageMargin)

	if s.theme.Name == "cyberpunk" {
		style = style.BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(s.theme.Accent.Color()).
			BorderLeft(true).
			PaddingLeft(1)
	}

	return style
}

func (s Style) AssistantBox() lipgloss.Style {
	style := lipgloss.NewStyle().
		Background(s.theme.AssistantBg.Color()).
		PaddingLeft(2).PaddingRight(2).
		MarginBottom(s.theme.MessageMargin)

	if s.theme.Name == "cyberpunk" {
		style = style.BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(s.theme.Bordered.Color()).
			BorderLeft(true).
			PaddingLeft(1)
	}

	return style
}

func (s Style) NoticeBox(level string) lipgloss.Style {
	var bg, fg color.Color

	switch level {
	case "error":
		bg = s.theme.ErrorBg.Color()
		fg = s.theme.Error.Color()
	case "warning":
		bg = s.theme.WarningBg.Color()
		fg = s.theme.Warning.Color()
	case "success":
		bg = s.theme.SuccessBg.Color()
		fg = s.theme.Success.Color()
	case "info", "system":
		fallthrough
	default:
		bg = s.theme.InfoBg.Color()
		fg = s.theme.Info.Color()
	}

	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		PaddingLeft(2).PaddingRight(2).
		MarginBottom(s.theme.MessageMargin)
}

func (s Style) WorkingIndicator() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.WorkingColor.Color()).
		Italic(true)
}

func (s Style) StatusIdle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.theme.Dim.Color())
}

func (s Style) StatusWorking() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.theme.WorkingColor.Color())
}

func (s Style) Dim() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.theme.Dim.Color())
}

func (s Style) Muted() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.theme.Muted.Color())
}

func (s Style) UserMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(s.theme.UserMsgBg.Color()).
		PaddingLeft(2).
		MarginBottom(1)
}

func (s Style) AssistantMessage() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(s.theme.AssistantBg.Color()).
		PaddingLeft(2).
		MarginBottom(1)
}

func (s Style) NoticeMsg(level string) lipgloss.Style {
	var bg, fg color.Color

	switch level {
	case "error":
		bg = s.theme.ErrorBg.Color()
		fg = s.theme.Error.Color()
	case "warning":
		bg = s.theme.WarningBg.Color()
		fg = s.theme.Warning.Color()
	case "success":
		bg = s.theme.SuccessBg.Color()
		fg = s.theme.Success.Color()
	case "info", "system":
		fallthrough
	default:
		bg = s.theme.InfoBg.Color()
		fg = s.theme.Info.Color()
	}

	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		PaddingLeft(2).
		MarginBottom(1)
}

func (s Style) ThinkingBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.ThinkingText.Color()).
		Italic(true).
		PaddingLeft(2).
		MarginBottom(1)
}

// ToolCall returns a style for tool call card headers.
func (s Style) ToolCall() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.theme.AccentText.Color())
}

// ToolRunningBgColor returns the background color for running tool calls.
func (s Style) ToolRunningBgColor() color.Color {
	return s.theme.ToolRunningBg.Color()
}

// ToolSuccessBgColor returns the background color for successful tool calls.
func (s Style) ToolSuccessBgColor() color.Color {
	return s.theme.ToolSuccessBg.Color()
}

// ToolFailureBgColor returns the background color for failed tool calls.
func (s Style) ToolFailureBgColor() color.Color {
	return s.theme.ToolFailureBg.Color()
}

// Bordered returns the theme's bordered color.
func (s Style) Bordered() color.Color {
	return s.theme.Bordered.Color()
}

// FooterPaddingX returns the theme's footer padding.
func (s Style) FooterPaddingX() int {
	return s.theme.FooterPaddingX
}

// Color accessors for use in external components (table, progress, etc.).
func (s Style) AccentColor() color.Color {
	return s.theme.Accent.Color()
}

func (s Style) AccentTextColor() color.Color {
	return s.theme.AccentText.Color()
}

func (s Style) MutedColor() color.Color {
	return s.theme.Muted.Color()
}

func (s Style) MutedTextColor() color.Color {
	return s.theme.MutedText.Color()
}

// PanelBgColor returns the background color for panel-style elements (modals, etc.).
func (s Style) PanelBgColor() color.Color {
	return s.theme.AssistantBg.Color()
}

// LoadTheme reads a theme from a JSON or YAML file.
// Supports both .json and .yaml/.yml extensions.
func LoadTheme(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read theme %s: %w", path, err)
	}

	var theme Theme
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &theme); err != nil {
			return nil, fmt.Errorf("parse theme %s as JSON: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &theme); err != nil {
			return nil, fmt.Errorf("parse theme %s as YAML: %w", path, err)
		}
	default:
		// Try JSON first, then YAML
		if err := json.Unmarshal(data, &theme); err != nil {
			if err := yaml.Unmarshal(data, &theme); err != nil {
				return nil, fmt.Errorf("parse theme %s: unsupported format", path)
			}
		}
	}

	if theme.Name == "" {
		theme.Name = filepath.Base(path)
	}

	return &theme, nil
}

// SaveTheme writes a theme to a JSON file.
func SaveTheme(path string, t *Theme) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal theme: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create theme dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write theme %s: %w", path, err)
	}

	return nil
}

// ParseAdaptiveColor parses a hex color string into an AdaptiveColor.
// Supports #RGB, #RRGGBB, #RRGGBBAA formats.
func ParseAdaptiveColor(hex string) (AdaptiveColor, error) {
	hex = strings.TrimSpace(hex)
	if hex == "" {
		return AdaptiveColor{}, fmt.Errorf("empty color string")
	}

	if !strings.HasPrefix(hex, "#") {
		return AdaptiveColor{}, fmt.Errorf("invalid color: missing # prefix: %s", hex)
	}

	r, g, b := 0, 0, 0
	switch len(hex) {
	case 4: // #RGB
		r = hexToNibble(hex[1])
		r = r<<4 | r
		g = hexToNibble(hex[2])
		g = g<<4 | g
		b = hexToNibble(hex[3])
		b = b<<4 | b
	case 7: // #RRGGBB
		cr, cg, cb, err := parseRGB(hex)
		if err != nil {
			return AdaptiveColor{}, err
		}
		r, g, b = cr, cg, cb
	case 9: // #RRGGBBAA
		cr, cg, cb, err := parseRGB(hex[:7])
		if err != nil {
			return AdaptiveColor{}, err
		}
		r, g, b = cr, cg, cb
		// Alpha channel is currently ignored
	default:
		return AdaptiveColor{}, fmt.Errorf("invalid color length: %s", hex)
	}

	return AdaptiveColor{
		Light: fmt.Sprintf("#%02x%02x%02x", r, g, b),
		Dark:  fmt.Sprintf("#%02x%02x%02x", r, g, b),
	}, nil
}

func hexToNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	}
	return 0
}


func parseRGB(hex string) (int, int, int, error) {
	if len(hex) != 7 {
		return 0, 0, 0, fmt.Errorf("invalid hex color: %s", hex)
	}
	r, err1 := strconv.ParseInt(hex[1:3], 16, 64)
	g, err2 := strconv.ParseInt(hex[3:5], 16, 64)
	b, err3 := strconv.ParseInt(hex[5:7], 16, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color: %s", hex)
	}
	return int(r), int(g), int(b), nil
}
