package themes

// Bundled returns the four built-in themes.
func Bundled() map[string]*Theme {
	return map[string]*Theme{
		"dark":       DarkTheme(),
		"light":      LightTheme(),
		"cyberpunk":  CyberpunkTheme(),
		"synthwave":  SynthwaveTheme(),
	}
}

// DarkTheme is a clean, professional dark theme.
func DarkTheme() *Theme {
	return &Theme{
		Name: "dark",
		Accent:   AdaptiveColor{Light: "#6366f1", Dark: "#818cf8"}, // Indigo
		Bordered: AdaptiveColor{Light: "#cbd5e1", Dark: "#27272a"}, // Zinc 800
		Muted:    AdaptiveColor{Light: "#64748b", Dark: "#52525b"}, // Zinc 600
		Dim:      AdaptiveColor{Light: "#94a3b8", Dark: "#3f3f46"}, // Zinc 700
		Success:  AdaptiveColor{Light: "#10b981", Dark: "#34d399"}, // Emerald
		Error:    AdaptiveColor{Light: "#ef4444", Dark: "#f87171"},
		Warning:  AdaptiveColor{Light: "#f59e0b", Dark: "#fbbf24"},
		Info:     AdaptiveColor{Light: "#0ea5e9", Dark: "#38bdf8"}, // Sky
		AccentText:   AdaptiveColor{Light: "#4338ca", Dark: "#c7d2fe"}, // Indigo
		MutedText:    AdaptiveColor{Light: "#475569", Dark: "#a1a1aa"}, // Zinc 400
		DimText:      AdaptiveColor{Light: "#94a3b8", Dark: "#71717a"}, // Zinc 500
		ThinkingText: AdaptiveColor{Light: "#64748b", Dark: "#52525b"},
		WorkingColor: AdaptiveColor{Light: "#6366f1", Dark: "#818cf8"},
		UserMsgBg:   AdaptiveColor{Light: "#f8fafc", Dark: "#18181b"}, // Zinc 900
		AssistantBg: AdaptiveColor{Light: "#e0e7ff", Dark: "#1e1b4b"}, // Deep Indigo
		ErrorBg:       AdaptiveColor{Light: "#fef2f2", Dark: "#450a0a"},
		WarningBg:     AdaptiveColor{Light: "#fffbeb", Dark: "#451a03"},
		InfoBg:        AdaptiveColor{Light: "#f0f9ff", Dark: "#082f49"},
		SuccessBg:     AdaptiveColor{Light: "#f0fdf4", Dark: "#064e3b"},
		ToolRunningBg: AdaptiveColor{Light: "#f1f5f9", Dark: "#27272a"},
		ToolSuccessBg: AdaptiveColor{Light: "#ecfdf5", Dark: "#064e3b"},
		ToolFailureBg: AdaptiveColor{Light: "#fef2f2", Dark: "#450a0a"},
		HeaderMarginTop: 1,
		FooterPaddingX:  0,
		MessageMargin:   1,
		ChatPaddingX:    0,
	}
}

// LightTheme is a clean, professional light theme.
func LightTheme() *Theme {
	return &Theme{
		Name: "light",
		Accent:   AdaptiveColor{Light: "#2563eb", Dark: "#3b82f6"}, // Blue
		Bordered: AdaptiveColor{Light: "#e2e8f0", Dark: "#475569"}, // Slate 200
		Muted:    AdaptiveColor{Light: "#94a3b8", Dark: "#64748b"}, // Slate 400
		Dim:      AdaptiveColor{Light: "#cbd5e1", Dark: "#94a3b8"}, // Slate 300
		Success:  AdaptiveColor{Light: "#16a34a", Dark: "#22c55e"},
		Error:    AdaptiveColor{Light: "#dc2626", Dark: "#ef4444"},
		Warning:  AdaptiveColor{Light: "#d97706", Dark: "#f59e0b"},
		Info:     AdaptiveColor{Light: "#0284c7", Dark: "#0ea5e9"},
		AccentText:   AdaptiveColor{Light: "#1e3a8a", Dark: "#bfdbfe"},
		MutedText:    AdaptiveColor{Light: "#64748b", Dark: "#94a3b8"},
		DimText:      AdaptiveColor{Light: "#94a3b8", Dark: "#cbd5e1"},
		ThinkingText: AdaptiveColor{Light: "#94a3b8", Dark: "#64748b"},
		WorkingColor: AdaptiveColor{Light: "#2563eb", Dark: "#3b82f6"},
		UserMsgBg:   AdaptiveColor{Light: "#ffffff", Dark: "#0f172a"}, // White
		AssistantBg: AdaptiveColor{Light: "#eff6ff", Dark: "#1e293b"}, // Blue 50
		ErrorBg:       AdaptiveColor{Light: "#fef2f2", Dark: "#450a0a"},
		WarningBg:     AdaptiveColor{Light: "#fffbeb", Dark: "#451a03"},
		InfoBg:        AdaptiveColor{Light: "#f0f9ff", Dark: "#082f49"},
		SuccessBg:     AdaptiveColor{Light: "#f0fdf4", Dark: "#064e3b"},
		ToolRunningBg: AdaptiveColor{Light: "#f8fafc", Dark: "#1e293b"},
		ToolSuccessBg: AdaptiveColor{Light: "#f0fdf4", Dark: "#064e3b"},
		ToolFailureBg: AdaptiveColor{Light: "#fef2f2", Dark: "#450a0a"},
		HeaderMarginTop: 1,
		FooterPaddingX:  0,
		MessageMargin:   1,
		ChatPaddingX:    0,
	}
}

// CyberpunkTheme is a high-contrast neon-on-black theme with aggressive accents.
func CyberpunkTheme() *Theme {
	return &Theme{
		Name: "cyberpunk",
		Accent:   AdaptiveColor{Light: "#33ff33", Dark: "#33ff33"}, // Bright Green
		Bordered: AdaptiveColor{Light: "#00ff00", Dark: "#00ff00"}, // CRT Green
		Muted:    AdaptiveColor{Light: "#008800", Dark: "#008800"}, // Forest Green
		Dim:      AdaptiveColor{Light: "#004400", Dark: "#004400"}, // Dark Forest
		Success:  AdaptiveColor{Light: "#33ff33", Dark: "#33ff33"},
		Error:    AdaptiveColor{Light: "#ff0000", Dark: "#ff0000"}, // Still need red for errors?
		Warning:  AdaptiveColor{Light: "#ffff00", Dark: "#ffff00"}, // Still need yellow for warnings?
		Info:     AdaptiveColor{Light: "#33ff33", Dark: "#33ff33"},
		AccentText:   AdaptiveColor{Light: "#aaffaa", Dark: "#aaffaa"}, // Light Green
		MutedText:    AdaptiveColor{Light: "#008800", Dark: "#008800"},
		DimText:      AdaptiveColor{Light: "#004400", Dark: "#004400"},
		ThinkingText: AdaptiveColor{Light: "#006600", Dark: "#006600"},
		WorkingColor: AdaptiveColor{Light: "#33ff33", Dark: "#33ff33"},
		UserMsgBg:   AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"}, // Lighter Gray
		AssistantBg: AdaptiveColor{Light: "#003300", Dark: "#002800"}, // Lighter Green
		ErrorBg:       AdaptiveColor{Light: "#1a0000", Dark: "#0d0000"},
		WarningBg:     AdaptiveColor{Light: "#1a1a00", Dark: "#0d0d00"},
		InfoBg:        AdaptiveColor{Light: "#001a00", Dark: "#000d00"},
		SuccessBg:     AdaptiveColor{Light: "#001a00", Dark: "#000d00"},
		ToolRunningBg: AdaptiveColor{Light: "#002200", Dark: "#001100"},
		ToolSuccessBg: AdaptiveColor{Light: "#003300", Dark: "#001a00"},
		ToolFailureBg: AdaptiveColor{Light: "#330000", Dark: "#1a0000"},
		HeaderMarginTop: 1,
		FooterPaddingX:  0,
		MessageMargin:   1,
		ChatPaddingX:    0,
	}
}

// SynthwaveTheme is a retro 80s purple-pink-blue gradient aesthetic.
func SynthwaveTheme() *Theme {
	return &Theme{
		Name: "synthwave",
		Accent:   AdaptiveColor{Light: "#ff71ce", Dark: "#ff71ce"}, // hot pink
		Bordered: AdaptiveColor{Light: "#05d9e8", Dark: "#05d9e8"}, // cyan
		Muted:    AdaptiveColor{Light: "#d9519b", Dark: "#d9519b"}, // muted pink
		Dim:      AdaptiveColor{Light: "#7b68ee", Dark: "#7b68ee"}, // slate blue
		Success:  AdaptiveColor{Light: "#05d9e8", Dark: "#05d9e8"},
		Error:    AdaptiveColor{Light: "#ff0044", Dark: "#ff0044"}, // deep red-pink
		Warning:  AdaptiveColor{Light: "#f5e556", Dark: "#f5e556"}, // yellow
		Info:     AdaptiveColor{Light: "#05d9e8", Dark: "#05d9e8"},
		AccentText:   AdaptiveColor{Light: "#ff71ce", Dark: "#ff71ce"},
		MutedText:    AdaptiveColor{Light: "#d9519b", Dark: "#d9519b"},
		DimText:      AdaptiveColor{Light: "#7b68ee", Dark: "#7b68ee"},
		ThinkingText: AdaptiveColor{Light: "#888888", Dark: "#666666"},
		WorkingColor: AdaptiveColor{Light: "#ff71ce", Dark: "#ff71ce"},
		UserMsgBg:   AdaptiveColor{Light: "#1a0a2e", Dark: "#0f0520"}, // darker purple
		AssistantBg: AdaptiveColor{Light: "#2a1b3d", Dark: "#1a0f2e"}, // deep purple
		ErrorBg:       AdaptiveColor{Light: "#2e0a1a", Dark: "#1f0510"}, // red-purple
		WarningBg:     AdaptiveColor{Light: "#2e2e0a", Dark: "#1f1f05"}, // yellow tint
		InfoBg:        AdaptiveColor{Light: "#0a2e2e", Dark: "#051f1f"}, // cyan tint
		SuccessBg:     AdaptiveColor{Light: "#0a2e2e", Dark: "#051f1f"}, // cyan tint (success uses cyan here)
		ToolRunningBg: AdaptiveColor{Light: "#1a1a2e", Dark: "#0f0f1f"}, // dark slate
		ToolSuccessBg: AdaptiveColor{Light: "#0a1a2e", Dark: "#050f1f"}, // blue-purple
		ToolFailureBg: AdaptiveColor{Light: "#2a0a1a", Dark: "#1f0510"}, // red-purple
		HeaderMarginTop: 1,
		FooterPaddingX:  0,
		MessageMargin:   1,
		ChatPaddingX:    0,
	}
}
