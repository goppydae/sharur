// Package agent provides the stateful agent with transcript, tools, and events.
package agent

import (
	"github.com/goppydae/gollm/internal/types"
)

// ThinkingLevel is an alias for types.ThinkingLevel.
type ThinkingLevel = types.ThinkingLevel

const (
	ThinkingOff    = types.ThinkingOff
	ThinkingLow    = types.ThinkingLow
	ThinkingMedium = types.ThinkingMedium
	ThinkingHigh   = types.ThinkingHigh
)

// Message is an alias for types.Message.
type Message = types.Message

// Image is an alias for types.Image.
type Image = types.Image

// ToolCall is an alias for types.ToolCall.
type ToolCall = types.ToolCall

// ToolOutput is an alias for types.ToolOutput.
type ToolOutput = types.ToolOutput

// ToolInfo is an alias for types.ToolInfo.
type ToolInfo = types.ToolInfo

// Session is an alias for types.Session.
type Session = types.Session

// AgentState holds the full state of an agent instance.
type AgentState struct {
	Session      Session         `json:"session"`
	SystemPrompt string          `json:"systemPrompt"`
	Messages      []Message       `json:"messages"`
	SteerQueue    []Message       `json:"steerQueue,omitempty"`
	FollowUpQueue []Message       `json:"followUpQueue,omitempty"`
	Tools         []ToolInfo      `json:"tools,omitempty"`
	Model         string          `json:"model"`
	Provider      string          `json:"provider"`
	Thinking      ThinkingLevel   `json:"thinkingLevel"`
	MaxTokens     int             `json:"maxTokens,omitempty"`
	Temperature   float64         `json:"temperature,omitempty"`
}
