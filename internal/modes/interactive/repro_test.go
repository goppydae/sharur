package interactive

import (
	"strings"
	"testing"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/themes"
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

	tcID := "call_123"

	// Manually build history entries (mirrors what syncHistoryFromService would produce)
	assistantEntry := historyEntry{
		role: "assistant",
		items: []contentItem{
			{kind: contentItemText, text: "I will read the file."},
			{kind: contentItemToolCall, tc: toolCallEntry{id: tcID, name: "read", arg: "test.go", status: toolCallRunning}},
		},
	}
	m.history = append(m.history, assistantEntry)

	// Simulate tool output arriving
	for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
		entry := &m.history[hIdx]
		for i := range entry.items {
			if entry.items[i].kind == contentItemToolCall && entry.items[i].tc.id == tcID {
				entry.items[i].tc.status = toolCallSuccess
				outItem := contentItem{
					kind: contentItemToolOutput,
					out: toolOutputEntry{
						toolCallID: tcID,
						content:    "package main\n\nfunc main() {}\n",
					},
				}
				entry.items = append(entry.items, contentItem{})
				copy(entry.items[i+2:], entry.items[i+1:])
				entry.items[i+1] = outItem
				break
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

	events := []*pb.AgentEvent{
		{Payload: &pb.AgentEvent_AgentStart{AgentStart: &pb.AgentStartEvent{}}},
		{Payload: &pb.AgentEvent_TextDelta{TextDelta: &pb.TextDeltaEvent{Content: "I will read the file and then list the directory.\n"}}},
		{Payload: &pb.AgentEvent_ToolCall{ToolCall: &pb.ToolCallEvent{Id: tc1ID, Name: "read", ArgsJson: `{"path":"main.go"}`}}},
		{Payload: &pb.AgentEvent_ToolCall{ToolCall: &pb.ToolCallEvent{Id: tc2ID, Name: "ls", ArgsJson: `{"path":"."}`}}},
		{Payload: &pb.AgentEvent_MessageEnd{MessageEnd: &pb.MessageEndEvent{}}},
		{Payload: &pb.AgentEvent_ToolDelta{ToolDelta: &pb.ToolDeltaEvent{ToolCallId: tc1ID, Content: "package main\n"}}},
		{Payload: &pb.AgentEvent_ToolOutput{ToolOutput: &pb.ToolOutputEvent{ToolCallId: tc1ID, ToolName: "read", Content: "package main\n\nfunc main() {}\n"}}},
		{Payload: &pb.AgentEvent_ToolOutput{ToolOutput: &pb.ToolOutputEvent{ToolCallId: tc2ID, ToolName: "ls", Content: "main.go\ngo.mod\n"}}},
	}

	for _, ev := range events {
		m.handleAgentEvent(ev)
	}

	res := m.buildChatContent()
	t.Logf("Final rendered output:\n%s", res)

	if !strings.Contains(res, "read") || !strings.Contains(res, "main.go") {
		t.Error("Expected 'read' and 'main.go' in rendered output")
	}
	if !strings.Contains(res, "ls") || !strings.Contains(res, ".") {
		t.Error("Expected 'ls' and '.' in rendered output")
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

	events := []*pb.AgentEvent{
		{Payload: &pb.AgentEvent_AgentStart{AgentStart: &pb.AgentStartEvent{}}},
		{Payload: &pb.AgentEvent_ToolCall{ToolCall: &pb.ToolCallEvent{Id: tcID, Name: "read", ArgsJson: `{"path":"main.go"}`}}},
		{Payload: &pb.AgentEvent_MessageEnd{MessageEnd: &pb.MessageEndEvent{}}},
		{Payload: &pb.AgentEvent_MessageEnd{MessageEnd: &pb.MessageEndEvent{}}}, // redundant
		{Payload: &pb.AgentEvent_ToolCall{ToolCall: &pb.ToolCallEvent{Id: tcID, Name: "read", ArgsJson: `{"path":"main.go"}`}}}, // redundant
		{Payload: &pb.AgentEvent_ToolOutput{ToolOutput: &pb.ToolOutputEvent{ToolCallId: tcID, ToolName: "read", Content: "package main\n"}}},
	}

	for _, ev := range events {
		m.handleAgentEvent(ev)
	}

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
	if !strings.Contains(res, "read") || !strings.Contains(res, "main.go") {
		t.Error("Expected read tool call and path in output")
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

func TestRenderErrorCapitalization(t *testing.T) {
	m := &model{}
	m.style = NewStyle(*themes.DarkTheme())
	m.width = 100
	m.vp.SetWidth(m.width - borderOffset - chatMargin*2)

	entry := historyEntry{
		role: "error",
		items: []contentItem{
			{kind: contentItemText, text: "something went wrong"},
		},
	}

	res := renderEntry(entry, m.style, m.width, false)
	t.Logf("Rendered error:\n%s", res)

	if !strings.Contains(res, "Something went wrong") {
		t.Error("Expected error message to be capitalized")
	}
}

