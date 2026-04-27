package interactive

import (
	"context"
	"testing"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"google.golang.org/grpc"
)

type noticeMockClient struct {
	stubClient
	msgs []*pb.ConversationMessage
}

func (c *noticeMockClient) GetMessages(ctx context.Context, in *pb.GetMessagesRequest, opts ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	return &pb.GetMessagesResponse{Messages: c.msgs}, nil
}

func TestSyncHistory_PreserveNotices(t *testing.T) {
	client := &noticeMockClient{
		msgs: []*pb.ConversationMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
	}
	m := &model{
		client:    client,
		sessionID: "test",
		history: []historyEntry{
			{role: "user", items: []contentItem{{kind: contentItemText, text: "Hello"}}},
			{role: "assistant", items: []contentItem{{kind: contentItemText, text: "Hi"}}},
			{role: "info", items: []contentItem{{kind: contentItemText, text: "Some notice"}}},
			{role: "error", items: []contentItem{{kind: contentItemText, text: "Some error"}}},
		},
	}

	m.syncHistoryFromService()

	// After sync, the notices should be preserved at the bottom.
	if len(m.history) != 4 {
		t.Errorf("Expected 4 messages in history, got %d", len(m.history))
	}
	if m.history[2].role != "info" || m.history[3].role != "error" {
		t.Errorf("Notices not preserved at bottom in correct order")
	}
}

func TestSyncHistory_PreserveRunningAssistant(t *testing.T) {
	client := &noticeMockClient{
		msgs: []*pb.ConversationMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	m := &model{
		client:    client,
		sessionID: "test",
		isRunning: true,
		history: []historyEntry{
			{role: "user", items: []contentItem{{kind: contentItemText, text: "Hello"}}},
			{role: "assistant", items: []contentItem{{kind: contentItemText, text: "Partial assistant response"}}},
		},
	}

	m.syncHistoryFromService()

	// After sync, the running assistant message should be preserved at the end.
	if len(m.history) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(m.history))
	}
	if m.history[1].role != "assistant" {
		t.Errorf("Expected trailing assistant entry, got %q", m.history[1].role)
	}
	if m.history[1].items[0].text != "Partial assistant response" {
		t.Errorf("Expected preserved text, got %q", m.history[1].items[0].text)
	}
}
