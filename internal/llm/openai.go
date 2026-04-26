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

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	temp      float64
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
// baseURL defaults to the official OpenAI endpoint.
func NewOpenAIProvider(baseURL, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		client:    &http.Client{Timeout: 5 * time.Minute},
		baseURL:   strings.TrimRight(baseURL, "/"),
		model:     model,
		maxTokens: 4096,
		temp:      0.7,
	}
}

// NewOpenAIProviderWithKey creates an OpenAI provider with an API key.
func NewOpenAIProviderWithKey(baseURL, model, apiKey string) *OpenAIProvider {
	p := NewOpenAIProvider(baseURL, model)
	p.apiKey = apiKey
	return p
}

func (p *OpenAIProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:          "openai",
		Model:         p.model,
		MaxTokens:     p.maxTokens,
		ContextWindow: GetContextWindow(p.model),
		HasToolCall:   true,
		HasImages:     true,
	}
}

func (p *OpenAIProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan *Event, error) {
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

func (p *OpenAIProvider) stream(ctx context.Context, req *CompletionRequest, ch chan<- *Event) error {
	msgs := convertMessagesForOpenAI(req.Messages)
	if req.System != "" {
		msgs = append([]map[string]any{{"role": "system", "content": req.System}}, msgs...)
	}

	body := map[string]any{
		"model":    p.model,
		"messages": msgs,
		"stream":   true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  json.RawMessage(t.Schema),
				},
			}
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if n := req.MaxTokens; n > 0 {
		body["max_tokens"] = n
	} else {
		body["max_tokens"] = p.maxTokens
	}
	if t := req.Temperature; t > 0 {
		body["temperature"] = t
	} else {
		body["temperature"] = p.temp
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// tool call accumulator: index → accumulated call
	type tcAcc struct {
		id   string
		name string
		args strings.Builder
	}
	tcMap := map[int]*tcAcc{}

	ch <- &Event{Type: EventMessageStart}

	err = StreamHTTP(ctx, p.client, httpReq, ch, func(line string) error {
		if line == "data: [DONE]" {
			return nil // finish
		}
		if !strings.HasPrefix(line, "data: ") {
			return nil
		}
		raw := line[6:]

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
					ReasoningText    string `json:"reasoning_text"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			return nil
		}
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				ch <- &Event{
					Type: EventMessageEnd,
					Usage: &Usage{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					},
				}
			}
			return nil
		}

		delta := chunk.Choices[0].Delta

		var reasoning string
		if delta.ReasoningContent != "" {
			reasoning = delta.ReasoningContent
		} else if delta.Reasoning != "" {
			reasoning = delta.Reasoning
		} else if delta.ReasoningText != "" {
			reasoning = delta.ReasoningText
		}
		if reasoning != "" {
			ch <- &Event{Type: EventThinkingDelta, Content: reasoning}
		}

		if delta.Content != "" {
			ch <- &Event{Type: EventTextDelta, Content: delta.Content}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := tcMap[tc.Index]
			if !ok {
				acc = &tcAcc{}
				tcMap[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Emit accumulated tool calls
	for idx, acc := range tcMap {
		ch <- &Event{
			Type: EventToolCall,
			ToolCall: &ToolCall{
				ID:       acc.id,
				Name:     acc.name,
				Args:     json.RawMessage(acc.args.String()),
				Position: idx,
			},
		}
	}

	return nil
}

// convertMessagesForOpenAI converts types.Message slice to OpenAI API format.
func convertMessagesForOpenAI(messages []types.Message) []map[string]any {
	var out []map[string]any
	for _, m := range messages {
		switch m.Role {
		case "tool":
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
		case "assistant":
			msg := map[string]any{
				"role":    "assistant",
				"content": m.Content,
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]any, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					tcs[i] = map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": string(tc.Args),
						},
					}
				}
				msg["tool_calls"] = tcs
			}
			out = append(out, msg)
		case "success":
			// Compaction summary — surface as a user turn so the model receives it.
			out = append(out, map[string]any{
				"role":    "user",
				"content": "[Context Summary]\n" + m.Content,
			})
		default:
			out = append(out, map[string]any{
				"role":    m.Role,
				"content": m.Content,
			})
		}
	}
	return out
}

func (p *OpenAIProvider) ListModels() ([]string, error) {
	url := p.baseURL + "/v1/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
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

var _ Provider = (*OpenAIProvider)(nil)
var _ ModelLister = (*OpenAIProvider)(nil)
