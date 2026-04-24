package interactive

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/themes"
	"github.com/goppydae/gollm/internal/tools"
)

// TestRenderOutput exercises the full rendering pipeline with a conversation.
// ANSI output is written to /tmp/gollm-render-output.txt for inspection.
func TestRenderOutput(t *testing.T) {
	registry := tools.NewToolRegistry()
	ag := agent.New(&stubProvider{}, registry)
	eventCh := make(chan agent.Event, 64)
	ag.Subscribe(func(ev agent.Event) {
		select {
		case eventCh <- ev:
		default:
		}
	})

	m := newModel("gpt-4", "test", "medium", 128000, ag, eventCh, session.NewManager(""), config.DefaultConfig(), "")
	m.style = NewStyle(*themes.DarkTheme())

	// Wide viewport so text isn't truncated
	m.onResize(160, 30)
	m.width = 160
	m.height = 30

	// Add test messages
	m.history = []historyEntry{
		{role: "user", items: []contentItem{{kind: contentItemText, text: "Hello, world!"}}},
		{role: "assistant", items: []contentItem{{kind: contentItemText, text: "Hi there! How can I help you today?"}}},
	}

	// Simulate agent running
	m.isRunning = true

	// Build chat content before rendering
	m.chatContent = m.buildChatContent()

	// Build and render
	view := m.View()
	output := view.Content

	// Write to file for inspection
	err := os.WriteFile("/tmp/gollm-render-output.txt", []byte(output), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Rendered %d bytes to /tmp/gollm-render-output.txt", len(output))

	// Check that ANSI escape codes are present
	if !strings.Contains(output, "\x1b[") {
		t.Error("Expected ANSI escape codes in output")
	}
	if !strings.Contains(output, "Hello, world!") {
		t.Error("Expected 'Hello, world!' in rendered output")
	}
	if !strings.Contains(output, "Hi there!") {
		t.Error("Expected 'Hi there!' in rendered output")
	}
}

// TestRenderInitialState renders the idle TUI with no messages.
func TestRenderInitialState(t *testing.T) {
	registry := tools.NewToolRegistry()
	ag := agent.New(&stubProvider{}, registry)
	eventCh := make(chan agent.Event, 64)

	m := newModel("llama3", "ollama", "low", 0, ag, eventCh, session.NewManager(""), config.DefaultConfig(), "")
	m.style = NewStyle(*themes.DarkTheme())
	m.onResize(100, 24)

	m.chatContent = m.buildChatContent()
	view := m.View()
	output := view.Content

	err := os.WriteFile("/tmp/gollm-render-initial.txt", []byte(output), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Rendered %d bytes to /tmp/gollm-render-initial.txt", len(output))
}

// TestRenderWithToolCalls renders with simulated tool calls in progress.
func TestRenderWithToolCalls(t *testing.T) {
	registry := tools.NewToolRegistry()
	ag := agent.New(&stubProvider{}, registry)
	eventCh := make(chan agent.Event, 64)

	m := newModel("gpt-4", "test", "medium", 128000, ag, eventCh, session.NewManager(""), config.DefaultConfig(), "")
	m.style = NewStyle(*themes.DarkTheme())
	m.onResize(160, 30)

	m.history = []historyEntry{
		{role: "user", items: []contentItem{{kind: contentItemText, text: "List the files in /tmp"}}},
		{role: "assistant", items: []contentItem{
			{kind: contentItemText, text: "Let me check what's in /tmp for you."},
			{kind: contentItemToolCall, tc: toolCallEntry{id: "call_1", name: "ls", arg: "/tmp", status: toolCallRunning}},
			{kind: contentItemToolCall, tc: toolCallEntry{id: "call_2", name: "grep", arg: "pattern /tmp", status: toolCallRunning}},
		}},
	}
	m.isRunning = true

	m.chatContent = m.buildChatContent()
	view := m.View()
	output := view.Content

	err := os.WriteFile("/tmp/gollm-render-toolcalls.txt", []byte(output), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Rendered %d bytes to /tmp/gollm-render-toolcalls.txt", len(output))
}

// stubProvider implements llm.Provider for testing.
type stubProvider struct{}

func (s *stubProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{
		Model: "stub",
		Name:  "stub",
	}
}

func (s *stubProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.Event, error) {
	ch := make(chan *llm.Event)
	close(ch)
	return ch, nil
}
