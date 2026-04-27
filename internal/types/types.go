// Package types holds shared types used across gollm packages.
package types

import (
	"encoding/json"
	"time"
)

// ThinkingLevel controls how much the model "thinks" before responding.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// Message represents a single message in the conversation.
type Message struct {
	ID         string     `json:"id,omitempty"` // Record ID in session tree
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Thinking   string     `json:"thinking,omitempty"`
	Images     []Image    `json:"images,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"` // set on role="tool" messages
	Timestamp  time.Time  `json:"timestamp,omitempty"`
	Usage      *Usage     `json:"usage,omitempty"`
}

// Image represents an image attachment.
type Image struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// ToolCall represents a function call from the LLM.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	Position int             `json:"position,omitempty"`
}

// ToolOutput represents the result of executing a tool call.
type ToolOutput struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Content    string `json:"content"`
	IsError    bool   `json:"isError"`
}

// ToolInfo describes a registered tool for the LLM.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema"`
}

// CompactionState stores the state of the latest context compaction.
type CompactionState struct {
	Summary          string `json:"summary"`
	FirstKeptEntryID string `json:"firstKeptEntryId"`
}

// Session represents a conversation session.
type Session struct {
	ID         string          `json:"id"`
	ParentID   *string         `json:"parentId"`
	Name       string          `json:"name"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
	Model      string          `json:"model"`
	Provider   string          `json:"provider"`
	Thinking   ThinkingLevel   `json:"thinkingLevel"`
	SystemPrompt string        `json:"systemPrompt"`
	Messages   []Message       `json:"messages"`
	Tools      []ToolInfo      `json:"tools,omitempty"`
	MaxTokens  int             `json:"maxTokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	IsRunning  bool            `json:"isRunning"`
	DryRun              bool   `json:"dryRun,omitempty"`
	CompactionEnabled   bool   `json:"compactionEnabled,omitempty"`
	CompactionReserve   int    `json:"compactionReserveTokens,omitempty"`
	CompactionKeep      int    `json:"compactionKeepRecentTokens,omitempty"`
	ParentMessageIndex  *int   `json:"parentMessageIndex,omitempty"`
	MergeSourceID       *string `json:"mergeSourceId,omitempty"`
	LatestCompaction    *CompactionState `json:"latestCompaction,omitempty"`
}

// Usage tracks token usage.
type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}
