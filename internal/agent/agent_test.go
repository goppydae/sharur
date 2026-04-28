package agent

import (
	"context"
	"testing"
	"time"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

func TestAgentSetters(t *testing.T) {
	prov := &mockProvider{}
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)

	ag.SetSystemPrompt("new system prompt")
	if ag.state.SystemPrompt != "new system prompt" {
		t.Errorf("expected new system prompt, got %s", ag.state.SystemPrompt)
	}

	ag.SetModel("new model")
	if ag.state.Model != "new model" {
		t.Errorf("expected new model, got %s", ag.state.Model)
	}

	ag.SetThinkingLevel(ThinkingHigh)
	if ag.state.Thinking != ThinkingHigh {
		t.Errorf("expected high thinking level, got %s", ag.state.Thinking)
	}

	ag.SetMaxTokens(100)
	if ag.state.MaxTokens != 100 {
		t.Errorf("expected max tokens 100, got %d", ag.state.MaxTokens)
	}

	ag.SetSessionName("test session")
	if ag.state.Session.Name != "test session" {
		t.Errorf("expected session name test session, got %s", ag.state.Session.Name)
	}

	ag.SetDryRun(true)
	if !ag.dryRun || !ag.state.DryRun {
		t.Error("expected dry run to be true")
	}
}

func TestAgentSubscribe(t *testing.T) {
	prov := &mockProvider{
		responses: []*llm.Event{
			{Type: llm.EventTextDelta, Content: "hello"},
		},
	}
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)

	events := make(chan Event, 10)
	unsub := ag.Subscribe(func(e Event) {
		events <- e
	})
	defer unsub()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_ = ag.Prompt(ctx, "hi")
	<-ag.Idle()

	foundStart := false
	foundEnd := false
	for i := 0; i < 10; i++ {
		select {
		case ev := <-events:
			if ev.Type == EventAgentStart {
				foundStart = true
			}
			if ev.Type == EventAgentEnd {
				foundEnd = true
			}
		case <-time.After(100 * time.Millisecond):
			goto done
		}
	}
done:
	if !foundStart {
		t.Error("EventAgentStart not received")
	}
	if !foundEnd {
		t.Error("EventAgentEnd not received")
	}
}

func TestAgentGetStats(t *testing.T) {
	prov := &mockProvider{}
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)

	ag.mu.Lock()
	ag.state.Messages = []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []types.ToolCall{{ID: "1", Name: "test"}}},
		{Role: "tool", Content: "result", ToolCallID: "1"},
	}
	ag.mu.Unlock()

	stats := ag.GetStats()
	if stats.UserMessages != 1 {
		t.Errorf("expected 1 user message, got %d", stats.UserMessages)
	}
	if stats.AssistantMsgs != 1 {
		t.Errorf("expected 1 assistant message, got %d", stats.AssistantMsgs)
	}
	if stats.ToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", stats.ToolCalls)
	}
	if stats.ToolResults != 1 {
		t.Errorf("expected 1 tool result, got %d", stats.ToolResults)
	}
}

func TestAgentReset(t *testing.T) {
	prov := &mockProvider{}
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)

	ag.mu.Lock()
	ag.state.Messages = []Message{{Role: "user", Content: "hello"}}
	ag.state.SteerQueue = []Message{{Role: "user", Content: "steer"}}
	ag.mu.Unlock()

	ag.Reset()

	if len(ag.state.Messages) != 0 {
		t.Error("expected 0 messages after reset")
	}
	if len(ag.state.SteerQueue) != 0 {
		t.Error("expected 0 steer queue messages after reset")
	}
}

func TestAgentResetSession(t *testing.T) {
	prov := &mockProvider{}
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)

	ag.mu.Lock()
	ag.state.Messages = []Message{{Role: "user", Content: "hello"}}
	ag.state.Session.ID = "old"
	ag.mu.Unlock()

	ag.ResetSession("new")

	if len(ag.state.Messages) != 0 {
		t.Error("expected 0 messages after reset session")
	}
	if ag.state.Session.ID != "new" {
		t.Errorf("expected session ID 'new', got %s", ag.state.Session.ID)
	}
}
