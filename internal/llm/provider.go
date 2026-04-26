// Package llm provides the LLM provider abstraction.
package llm

import (
	"context"
	"encoding/json"
	"github.com/goppydae/gollm/internal/types"
	"time"
)

// Provider is the unified LLM interface.
// All providers implement this — local or cloud.
type Provider interface {
	// Stream sends messages and returns an event stream.
	Stream(ctx context.Context, req *CompletionRequest) (<-chan *Event, error)

	// Info returns provider metadata.
	Info() ProviderInfo
}

// ModelLister is an optional interface providers can implement to
// list their available models (used by --list-models).
type ModelLister interface {
	ListModels() ([]string, error)
}

// CompletionRequest is a request to the LLM.
type CompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []types.Message         `json:"messages"`
	Tools       []types.ToolInfo        `json:"tools,omitempty"`
	System      string                  `json:"system,omitempty"`
	Thinking    types.ThinkingLevel     `json:"thinking,omitempty"`
	MaxTokens   int                     `json:"maxTokens,omitempty"`
	Temperature float64                 `json:"temperature,omitempty"`
	StreamOpts  StreamOptions           `json:"streamOpts,omitempty"`
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	ChunkSize int
	Timeout   time.Duration
}

// DefaultStreamOptions returns default streaming options.
func DefaultStreamOptions() StreamOptions {
	return StreamOptions{
		ChunkSize: 1,
		Timeout:   60 * time.Second,
	}
}

// ProviderInfo describes a provider's capabilities.
type ProviderInfo struct {
	Name          string
	Model         string
	MaxTokens     int
	ContextWindow int // 0 = unknown
	HasToolCall   bool
	HasImages     bool
}

// Event represents an LLM stream event.
type Event struct {
	Type     EventType
	Content  string
	ToolCall *ToolCall
	Usage    *Usage
	Error    error
}

// EventType identifies the kind of LLM event.
type EventType string

const (
	EventMessageStart  EventType = "message_start"
	EventTextDelta     EventType = "text_delta"
	EventToolCall      EventType = "tool_call"
	EventThinkingDelta EventType = "thinking_delta"
	EventMessageEnd    EventType = "message_end"
	EventError         EventType = "error"
)

// streamChannelBuf is the buffer size for provider event channels.
// Large enough to absorb a burst of tokens without blocking the HTTP goroutine.
const streamChannelBuf = 32

// ToolCall represents a function call from the LLM.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	Position int             `json:"position,omitempty"`
}

// Usage is now in internal/types
type Usage = types.Usage
