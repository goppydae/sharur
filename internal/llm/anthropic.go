package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/goppydae/gollm/internal/types"
)

const (
	anthropicAPIVersion = "2023-07-01"
	anthropicBaseURL    = "https://api.anthropic.com"

	// anthropicDefaultMaxTokens is the default output token limit when none is specified.
	anthropicDefaultMaxTokens = 8192

	// anthropicThinkingBudgetMedium / High are the extended-thinking token budgets.
	anthropicThinkingBudgetMedium = 10000
	anthropicThinkingBudgetHigh   = 20000

	// anthropicThinkingTemperature is required by the API when extended thinking is enabled.
	anthropicThinkingTemperature = 1.0

	// anthropicClientTimeout is the HTTP client timeout for streaming responses.
	anthropicClientTimeout = 5 * time.Minute
)

// AnthropicProvider implements the Provider interface for Anthropic's Messages API.
type AnthropicProvider struct {
	client    *http.Client
	apiKey    string
	model     string
	maxTokens int
	temp      float64
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return &AnthropicProvider{
		client:    &http.Client{Timeout: anthropicClientTimeout},
		apiKey:    apiKey,
		model:     model,
		maxTokens: anthropicDefaultMaxTokens,
		temp:      anthropicThinkingTemperature,
	}
}

func (p *AnthropicProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:          "anthropic",
		Model:         p.model,
		MaxTokens:     p.maxTokens,
		ContextWindow: GetContextWindow(p.model),
		HasToolCall:   true,
		HasImages:     true,
	}
}

func (p *AnthropicProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan *Event, error) {
	ch := make(chan *Event, streamChannelBuf)
	go func() {
		defer close(ch)
		if err := p.stream(ctx, req, ch); err != nil {
			select {
			case ch <- &Event{Type: EventError, Error: err}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (p *AnthropicProvider) stream(ctx context.Context, req *CompletionRequest, ch chan<- *Event) error {
	msgs := convertMessagesForAnthropic(req.Messages)

	body := map[string]any{
		"model":      p.model,
		"messages":   msgs,
		"stream":     true,
		"max_tokens": p.maxTokens,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	} else if p.temp > 0 {
		body["temperature"] = p.temp
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": json.RawMessage(t.Schema),
			}
		}
		body["tools"] = tools
	}
	// Extended thinking
	if req.Thinking == types.ThinkingHigh || req.Thinking == types.ThinkingMedium {
		budgetTokens := anthropicThinkingBudgetMedium
		if req.Thinking == types.ThinkingHigh {
			budgetTokens = anthropicThinkingBudgetHigh
		}
		body["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budgetTokens,
		}
		body["temperature"] = anthropicThinkingTemperature // required for extended thinking
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	// Track content blocks: index → type/id/name/args
	type blockState struct {
		blockType string // "text" or "tool_use" or "thinking"
		id        string
		name      string
		args      strings.Builder
	}
	blocks := map[int]*blockState{}

	ch <- &Event{Type: EventMessageStart}

	err = StreamHTTP(ctx, p.client, httpReq, ch, func(line string) error {
		if !strings.HasPrefix(line, "data: ") {
			return nil
		}
		raw := line[6:]

		var ev struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Delta json.RawMessage `json:"delta"`
			ContentBlock *struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Text  string `json:"text"`
			} `json:"content_block"`
			Message *struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			return nil
		}

		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock == nil {
				return nil
			}
			b := &blockState{blockType: ev.ContentBlock.Type}
			if ev.ContentBlock.ID != "" {
				b.id = ev.ContentBlock.ID
			}
			if ev.ContentBlock.Name != "" {
				b.name = ev.ContentBlock.Name
			}
			blocks[ev.Index] = b

		case "content_block_delta":
			b, ok := blocks[ev.Index]
			if !ok {
				return nil
			}
			var delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				ThinkingText string `json:"thinking"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err != nil {
				return nil
			}
			switch delta.Type {
			case "text_delta":
				ch <- &Event{Type: EventTextDelta, Content: delta.Text}
			case "input_json_delta":
				b.args.WriteString(delta.PartialJSON)
			case "thinking_delta":
				ch <- &Event{Type: EventThinkingDelta, Content: delta.ThinkingText}
			}

		case "message_delta":
			var d struct {
				Usage *struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(ev.Delta, &d); err == nil && d.Usage != nil {
				ch <- &Event{
					Type: EventMessageEnd,
					Usage: &Usage{
						CompletionTokens: d.Usage.OutputTokens,
					},
				}
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Emit completed tool calls
	for idx, b := range blocks {
		if b.blockType == "tool_use" {
			ch <- &Event{
				Type: EventToolCall,
				ToolCall: &ToolCall{
					ID:       b.id,
					Name:     b.name,
					Args:     json.RawMessage(b.args.String()),
					Position: idx,
				},
			}
		}
	}

	return nil
}

// convertMessagesForAnthropic converts types.Message slice to Anthropic API format.
// Tool results are merged into user messages; consecutive tool results are batched.
func convertMessagesForAnthropic(messages []types.Message) []map[string]any {
	var out []map[string]any
	i := 0
	for i < len(messages) {
		m := messages[i]
		switch m.Role {
		case "user":
			out = append(out, map[string]any{
				"role":    "user",
				"content": m.Content,
			})
			i++
		case "assistant":
			var content []map[string]any
			if m.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if len(tc.Args) > 0 {
					if err := json.Unmarshal(tc.Args, &input); err != nil {
						// Malformed args from the LLM — pass the raw string so the model can see the error.
						input = string(tc.Args)
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": content,
			})
			i++
		case "tool":
			// Batch all consecutive tool result messages into one user message.
			var results []map[string]any
			for i < len(messages) && messages[i].Role == "tool" {
				tm := messages[i]
				results = append(results, map[string]any{
					"type":        "tool_result",
					"tool_use_id": tm.ToolCallID,
					"content":     tm.Content,
				})
				i++
			}
			out = append(out, map[string]any{
				"role":    "user",
				"content": results,
			})
		case "success":
			// Compaction summary — surface as a user turn so the model receives it.
			out = append(out, map[string]any{
				"role":    "user",
				"content": "[Context Summary]\n" + m.Content,
			})
			i++
		default:
			i++
		}
	}
	return out
}

func (p *AnthropicProvider) ListModels() ([]string, error) {
	url := anthropicBaseURL + "/v1/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.String())
	}

	var data struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var names []string
	for _, m := range data.Data {
		names = append(names, m.ID)
	}
	return names, nil
}

var _ Provider = (*AnthropicProvider)(nil)
var _ ModelLister = (*AnthropicProvider)(nil)
