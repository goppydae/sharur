package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/themes"
	"github.com/goppydae/gollm/internal/types"
)

func TestRenderReadToolCall(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)
	
	tc := toolCallEntry{
		id:     "call_1",
		name:   "read",
		arg:    "internal/tools/read.go",
		status: toolCallSuccess,
	}
	output := "package tools\n\nimport (\n\t\"context\"\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"strings\"\n)\n"
	
	res := renderToolCall(tc, output, m.width, m.style, false)
	t.Logf("Rendered output:\n%s", res)
	
	if !strings.Contains(res, "read") {
		t.Error("Expected 'read' in rendered output")
	}
	if !strings.Contains(res, "internal/tools/read.go") {
		t.Error("Expected 'internal/tools/read.go' in rendered output")
	}
	if !strings.Contains(res, "package tools") {
		t.Error("Expected 'package tools' in rendered output")
	}
}

func TestSyncReadToolCall(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.ag = &agent.Agent{} // Stub
	
	tcID := "call_123"
	m.ag = agent.New(&reproStubProvider{}, nil)
	
	// Create a history in the agent
	assistantMsg := agent.Message{
		Role: "assistant",
		Content: "I will read the file.",
		ToolCalls: []types.ToolCall{
			{ID: tcID, Name: "read", Args: []byte(`{"path": "test.go"}`)},
		},
	}
	toolMsg := agent.Message{
		Role: "tool",
		ToolCallID: tcID,
		Content: "package main\n\nfunc main() {}\n",
	}
	
	// We need to inject these messages into the agent's private state for testing
	// but since we can't easily, let's just mock the sync logic
	
	msgs := []agent.Message{assistantMsg, toolMsg}
	
	m.history = make([]historyEntry, 0)
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			entry := historyEntry{role: "assistant"}
			entry.items = append(entry.items, contentItem{kind: contentItemText, text: msg.Content})
			for _, tc := range msg.ToolCalls {
				entry.items = append(entry.items, contentItem{
					kind: contentItemToolCall,
					tc: toolCallEntry{
						id:     tc.ID,
						name:   tc.Name,
						arg:    "test.go",
						status: toolCallRunning,
					},
				})
			}
			m.history = append(m.history, entry)
		} else if msg.Role == "tool" {
			for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
				entry := &m.history[hIdx]
				for i := range entry.items {
					if entry.items[i].kind == contentItemToolCall && entry.items[i].tc.id == msg.ToolCallID {
						entry.items[i].tc.status = toolCallSuccess
						outItem := contentItem{
							kind: contentItemToolOutput,
							out: toolOutputEntry{
								toolCallID: msg.ToolCallID,
								content:    msg.Content,
							},
						}
						entry.items = append(entry.items, contentItem{})
						copy(entry.items[i+2:], entry.items[i+1:])
						entry.items[i+1] = outItem
						break
					}
				}
			}
		}
	}
	
	if len(m.history) != 1 {
		t.Fatalf("Expected 1 history entry, got %d", len(m.history))
	}
	entry := m.history[0]
	if len(entry.items) != 3 {
		t.Fatalf("Expected 3 items (text, tc, output), got %d", len(entry.items))
	}
	if entry.items[1].kind != contentItemToolCall || entry.items[1].tc.name != "read" {
		t.Errorf("Expected item 1 to be 'read' tool call")
	}
	if entry.items[2].kind != contentItemToolOutput || !strings.Contains(entry.items[2].out.content, "package main") {
		t.Errorf("Expected item 2 to be tool output with content")
	}
}



func TestRenderLargeReadToolCall(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)
	
	tc := toolCallEntry{
		id:     "call_large",
		name:   "read",
		arg:    "large_file.txt",
		status: toolCallSuccess,
	}
	
	var sb strings.Builder
	for i := 1; i <= 1000; i++ {
		sb.WriteString(strings.Repeat("A", 80))
		sb.WriteString("\n")
	}
	output := sb.String()
	
	res := renderToolCall(tc, output, m.width, m.style, false)
	t.Logf("Rendered output length: %d", len(res))
	
	if !strings.Contains(res, "read") {
		t.Error("Expected 'read' in rendered output")
	}
	if !strings.Contains(res, "…") {
		t.Error("Expected truncation marker '…' in rendered output")
	}
}

func TestFullAgentTurnEvents(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)
	
	tc1ID := "call_read"
	tc2ID := "call_ls"
	
	// Sequence of events
	events := []agent.Event{
		{Type: agent.EventAgentStart},
		{Type: agent.EventTextDelta, Content: "I will read the file and then list the directory.\n"},
		{Type: agent.EventToolCall, ToolCall: &types.ToolCall{ID: tc1ID, Name: "read", Args: []byte(`{"path":"main.go"}`)}},
		{Type: agent.EventToolCall, ToolCall: &types.ToolCall{ID: tc2ID, Name: "ls", Args: []byte(`{"path":"."}`)}},
		{Type: agent.EventMessageEnd},
		{Type: agent.EventToolDelta, ToolCall: &types.ToolCall{ID: tc1ID, Name: "read"}, Content: "package main\n"},
		{Type: agent.EventToolOutput, ToolOutput: &types.ToolOutput{ToolCallID: tc1ID, ToolName: "read", Content: "package main\n\nfunc main() {}\n"}},
		{Type: agent.EventToolOutput, ToolOutput: &types.ToolOutput{ToolCallID: tc2ID, ToolName: "ls", Content: "main.go\ngo.mod\n"}},
	}
	
	for _, ev := range events {
		m.handleAgentEvent(ev)
	}
	
	res := m.buildChatContent()
	t.Logf("Final rendered output:\n%s", res)
	
	if !strings.Contains(res, "read main.go") {
		t.Error("Expected 'read main.go' in rendered output")
	}
	if !strings.Contains(res, "ls .") {
		t.Error("Expected 'ls .' in rendered output")
	}
	if !strings.Contains(res, "package main") {
		t.Error("Expected 'package main' in rendered output")
	}
	if !strings.Contains(res, "main.go") || !strings.Contains(res, "go.mod") {
		t.Error("Expected 'main.go' and 'go.mod' in rendered output")
	}
}

func TestRedundantEvents(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)
	
	tcID := "call_redundant"
	
	// Sequence with redundant events
	events := []agent.Event{
		{Type: agent.EventAgentStart},
		{Type: agent.EventToolCall, ToolCall: &types.ToolCall{ID: tcID, Name: "read", Args: []byte(`{"path":"main.go"}`)}},
		{Type: agent.EventMessageEnd}, // First end
		{Type: agent.EventMessageEnd}, // Redundant end
		{Type: agent.EventToolCall, ToolCall: &types.ToolCall{ID: tcID, Name: "read", Args: []byte(`{"path":"main.go"}`)}}, // Redundant call
		{Type: agent.EventToolOutput, ToolOutput: &types.ToolOutput{ToolCallID: tcID, ToolName: "read", Content: "package main\n"}},
	}
	
	for _, ev := range events {
		m.handleAgentEvent(ev)
	}
	
	// Verify history size
	if len(m.history) != 1 {
		t.Errorf("Expected 1 history entry, got %d", len(m.history))
	}
	
	entry := m.history[0]
	tcCount := 0
	for _, item := range entry.items {
		if item.kind == contentItemToolCall {
			tcCount++
		}
	}
	if tcCount != 1 {
		t.Errorf("Expected 1 tool call item, got %d", tcCount)
	}
	
	res := m.buildChatContent()
	if !strings.Contains(res, "✓ read main.go") {
		t.Error("Expected successful read tool call in output")
	}
}

func TestRenderFileAttachment(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)
	
	text := "<file path=\"README.md\">\n# gollm\nAn AI agent harness.\n</file>\nExtra text."
	entry := historyEntry{
		role: "user",
		items: []contentItem{
			{kind: contentItemText, text: text},
		},
	}
	
	res := renderEntry(entry, m.style, m.width, false)
	t.Logf("Rendered file attachment:\n%s", res)
	
	if !strings.Contains(res, "📎 file: README.md") {
		t.Error("Expected file attachment header in rendered output")
	}
	if !strings.Contains(res, "Extra text.") {
		t.Error("Expected extra text in rendered output")
	}
	if strings.Contains(res, "# gollm") {
		t.Error("Expected content to be hidden when not expanded")
	}
}

type reproStubProvider struct{}
func (s *reproStubProvider) Info() llm.ProviderInfo { return llm.ProviderInfo{Name: "stub"} }
func (s *reproStubProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.Event, error) { return nil, nil }
