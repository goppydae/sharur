package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// summaryProvider returns a fixed summary text as the LLM response.
type summaryProvider struct {
	summaryText string
	contextWindow int
}

func (s *summaryProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan *llm.Event, error) {
	ch := make(chan *llm.Event, 2)
	ch <- &llm.Event{Type: llm.EventTextDelta, Content: s.summaryText}
	close(ch)
	return ch, nil
}

func (s *summaryProvider) Info() llm.ProviderInfo {
	w := s.contextWindow
	if w == 0 {
		w = 100000
	}
	return llm.ProviderInfo{Name: "mock", Model: "test", ContextWindow: w}
}

func newAgentWithMessages(msgs []Message, prov llm.Provider) *Agent {
	reg := tools.NewToolRegistry()
	ag := New(prov, reg)
	ag.mu.Lock()
	ag.state.Messages = msgs
	ag.mu.Unlock()
	return ag
}

// TestCompact_CompactionNoticeAppearsInHistory verifies that after compaction
// the history contains a concise compaction notice instead of the full summary.
func TestCompact_CompactionNoticeAppearsInHistory(t *testing.T) {
	wantSummary := summarySentinel + "## Goal\nDo something\n"
	prov := &summaryProvider{summaryText: wantSummary}

	msgs := []Message{
		{ID: "m1", Role: "user", Content: "hello world", Timestamp: time.Now()},
		{ID: "m2", Role: "assistant", Content: strings.Repeat("word ", 5000), Timestamp: time.Now()},
		{ID: "m3", Role: "user", Content: "ok", Timestamp: time.Now()},
	}
	ag := newAgentWithMessages(msgs, prov)
	ag.SetCompactionConfig(true, 512, 512)

	ag.Compact(context.Background(), 512)

	got := ag.Messages()
	if len(got) < 4 {
		t.Errorf("expected at least 4 messages (3 original + 1 notice), got %d", len(got))
	}
	
	foundNotice := false
	for _, m := range got {
		if m.Role == "compaction" {
			foundNotice = true
			if !strings.Contains(m.Content, "Freed") {
				t.Errorf("compaction notice content = %q, want it to contain 'Freed'", m.Content)
			}
			break
		}
	}
	if !foundNotice {
		t.Error("compaction notice not found in history")
	}

	// Verify notice is the last message
	last := got[len(got)-1]
	if last.Role != "compaction" {
		t.Errorf("last message role = %s, want compaction", last.Role)
	}

	// Verify the actual summary is in LatestCompaction
	if ag.state.LatestCompaction == nil || !strings.HasPrefix(ag.state.LatestCompaction.Summary, wantSummary) {
		t.Errorf("summary not saved in LatestCompaction; got %v", ag.state.LatestCompaction)
	}
}

// TestCompact_ReducesLlmContextButPreservesHistory checks that LLM context is pruned
// while the full message history is kept in the agent state.
func TestCompact_ReducesLlmContextButPreservesHistory(t *testing.T) {
	prov := &summaryProvider{summaryText: summarySentinel + "## Goal\ntest\n"}

	var msgs []Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{ID: uuid.New().String(), Role: "user", Content: strings.Repeat("a", 400), Timestamp: time.Now()})
		msgs = append(msgs, Message{ID: uuid.New().String(), Role: "assistant", Content: strings.Repeat("b", 400), Timestamp: time.Now()})
	}
	ag := newAgentWithMessages(msgs, prov)

	before := len(msgs)
	ag.Compact(context.Background(), 1000)
	
	historyCount := len(ag.Messages())
	if historyCount <= before {
		t.Errorf("expected history count to increase (original + notice), got %d <= %d", historyCount, before)
	}

	llmMsgs := ag.buildLlmMessages()
	if len(llmMsgs) >= before {
		t.Errorf("LLM context did not reduce: llmMsgs=%d before=%d", len(llmMsgs), before)
	}
	
	if !strings.HasPrefix(llmMsgs[0].Content, summarySentinel) {
		t.Errorf("first LLM message should be the summary, got role=%q content=%q", llmMsgs[0].Role, llmMsgs[0].Content)
	}
}

// TestCompact_LifecycleStateRestoredAfterManualCompact verifies fix #2:
// the agent must return to StateIdle after a manual /compact while idle.
func TestCompact_LifecycleStateRestoredAfterManualCompact(t *testing.T) {
	prov := &summaryProvider{summaryText: summarySentinel + "## Goal\ntest\n"}
	msgs := []Message{
		{Role: "user", Content: strings.Repeat("x", 3000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("y", 3000), Timestamp: time.Now()},
		{Role: "user", Content: "next", Timestamp: time.Now()},
	}
	ag := newAgentWithMessages(msgs, prov)

	if ag.lifeState.Current() != StateIdle {
		t.Fatalf("pre-condition: expected StateIdle, got %s", ag.lifeState.Current())
	}

	ag.Compact(context.Background(), 512)

	if ag.lifeState.Current() != StateIdle {
		t.Errorf("after manual compact: expected StateIdle, got %s", ag.lifeState.Current())
	}
}

// TestFindCutPoint_NothingToCompact checks that findCutPoint returns start when
// all messages fit within the budget.
func TestFindCutPoint_NothingToCompact(t *testing.T) {
	ag := newAgentWithMessages(nil, &summaryProvider{})
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	res := ag.findCutPoint(msgs, 0, len(msgs), 100000)
	if res.FirstKeptIndex != 0 {
		t.Errorf("expected FirstKeptIndex=0 (nothing to cut), got %d", res.FirstKeptIndex)
	}
}

// TestFindCutPoint_AvoidsToolMessages checks that the cut point is never placed
// on a tool-result message.
func TestFindCutPoint_AvoidsToolMessages(t *testing.T) {
	ag := newAgentWithMessages(nil, &summaryProvider{})
	msgs := []Message{
		{Role: "user", Content: "do thing"},
		{Role: "assistant", Content: strings.Repeat("a", 800)},
		{Role: "tool", Content: strings.Repeat("b", 800), ToolCallID: "1"},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: "done"},
	}
	// small budget forces a cut near the middle
	res := ag.findCutPoint(msgs, 0, len(msgs), 600)
	if res.FirstKeptIndex < len(msgs) {
		role := msgs[res.FirstKeptIndex].Role
		if role == "tool" {
			t.Errorf("cut point landed on a tool message at index %d", res.FirstKeptIndex)
		}
	}
}

// TestEstimateMessageTokens_Images verifies fix #7: images add tokens.
func TestEstimateMessageTokens_Images(t *testing.T) {
	withoutImage := EstimateMessageTokens(Message{Content: "hello"})

	// A 750-byte base64 blob should add ~1024 tokens (floor).
	withImage := EstimateMessageTokens(Message{
		Content: "hello",
		Images:  []types.Image{{MIMEType: "image/png", Data: strings.Repeat("A", 750)}},
	})

	if withImage <= withoutImage {
		t.Errorf("image tokens not counted: without=%d with=%d", withoutImage, withImage)
	}
}

// TestParseFileActivityFromSummary verifies fix #10 parsing.
func TestParseFileActivityFromSummary(t *testing.T) {
	summary := summarySentinel + "## Goal\ntest\n\n### File Activity\n- Read: a.go, b.go\n- Modified: c.go\n"
	read, mod := parseFileActivityFromSummary(summary)
	if len(read) != 2 || read[0] != "a.go" || read[1] != "b.go" {
		t.Errorf("unexpected read files: %v", read)
	}
	if len(mod) != 1 || mod[0] != "c.go" {
		t.Errorf("unexpected modified files: %v", mod)
	}
}

// TestSerializeConversation_ToolCallLinkage verifies fix #8: tool-result messages
// include the originating call ID in the serialisation.
func TestSerializeConversation_ToolCallLinkage(t *testing.T) {
	ag := newAgentWithMessages(nil, &summaryProvider{})
	msgs := []Message{
		{Role: "assistant", Content: "calling", ToolCalls: []types.ToolCall{{ID: "tc1", Name: "read"}}},
		{Role: "tool", Content: "file contents", ToolCallID: "tc1"},
	}
	out := ag.serializeConversation(msgs)
	if !strings.Contains(out, "id=tc1") {
		t.Errorf("serialized output does not contain tool-call ID linkage:\n%s", out)
	}
}

// TestCompact_UpdatesExistingSummary verifies that a second compaction uses
// UPDATE_SUMMARIZATION_PROMPT (previousSummary is non-empty).
func TestCompact_UpdatesExistingSummary(t *testing.T) {
	var mu sync.Mutex
	var receivedPrompts []string
	prov := &captureProvider{
		reply: summarySentinel + "## Goal\nupdated\n",
		onPrompt: func(p string) {
			mu.Lock()
			receivedPrompts = append(receivedPrompts, p)
			mu.Unlock()
		},
	}

	existingSummary := summarySentinel + "## Goal\noriginal\n"
	msgs := []Message{
		{Role: "success", Content: existingSummary, Timestamp: time.Now()},
		{Role: "user", Content: strings.Repeat("x", 3000), Timestamp: time.Now()},
		{Role: "assistant", Content: strings.Repeat("y", 3000), Timestamp: time.Now()},
		{Role: "user", Content: "continue", Timestamp: time.Now()},
	}
	ag := newAgentWithMessages(msgs, prov)
	ag.mu.Lock()
	ag.state.LatestCompaction = &types.CompactionState{
		Summary:          existingSummary,
		FirstKeptEntryID: msgs[1].ID, // The first message after the summary
	}
	ag.mu.Unlock()
	ag.Compact(context.Background(), 512)

	mu.Lock()
	prompts := receivedPrompts
	mu.Unlock()

	found := false
	for _, p := range prompts {
		if strings.Contains(p, "previous-summary") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one UPDATE prompt with <previous-summary> tag; got prompts: %v", prompts)
	}
}

// captureProvider is a mock LLM provider that captures the full prompt text.
// onPrompt may be called concurrently (generateSummary and generateTurnPrefixSummary
// run in parallel inside Compact), so access is guarded by a mutex.
type captureProvider struct {
	mu       sync.Mutex
	reply    string
	onPrompt func(string)
}

func (c *captureProvider) Stream(_ context.Context, req *llm.CompletionRequest) (<-chan *llm.Event, error) {
	c.mu.Lock()
	if c.onPrompt != nil && len(req.Messages) > 0 {
		c.onPrompt(req.Messages[0].Content)
	}
	c.mu.Unlock()
	ch := make(chan *llm.Event, 2)
	ch <- &llm.Event{Type: llm.EventTextDelta, Content: c.reply}
	close(ch)
	return ch, nil
}

func (c *captureProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{Name: "mock", Model: "test", ContextWindow: 100000}
}
