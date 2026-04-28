package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GoogleProvider implements the Provider interface for Google Gemini APIs.
type GoogleProvider struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	temp      float64
}

// NewGoogleProvider creates a new Google Gemini provider.
func NewGoogleProvider(apiKey, model string) *GoogleProvider {
	return &GoogleProvider{
		client:    &http.Client{Timeout: 5 * time.Minute},
		baseURL:   "https://generativelanguage.googleapis.com",
		apiKey:    apiKey,
		model:     model,
		maxTokens: 4096,
		temp:      0.7,
	}
}

// WithBaseURL sets a custom base URL (useful for testing).
func (p *GoogleProvider) WithBaseURL(url string) *GoogleProvider {
	p.baseURL = strings.TrimRight(url, "/")
	return p
}

func (p *GoogleProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:          "google",
		Model:         p.model,
		MaxTokens:     p.maxTokens,
		ContextWindow: 1000000, // Gemini 1.5 Pro has 1M+
		HasToolCall:   true,
		HasImages:     true,
	}
}

func (p *GoogleProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan *Event, error) {
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

func (p *GoogleProvider) stream(ctx context.Context, req *CompletionRequest, ch chan<- *Event) error {
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent", p.baseURL, p.model)

	body := p.convertRequest(req)
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.String())
	}

	ch <- &Event{Type: EventMessageStart}

	// The Gemini streamGenerateContent returns a JSON array of objects over the stream.
	// We need a custom decoder that can handle the SSE-like framing or just the JSON stream.
	// Actually, Gemini v1beta streamGenerateContent sends a single JSON array that is streamed?
	// No, it's usually "chunked" where each chunk is a valid JSON object.
	
	// Use a scanner to read the JSON stream. Gemini sends chunks like [ { ... }, { ... } ]
	// But it's actually not a standard SSE. It's a JSON array where elements are sent over time.
	
	reader := bufio.NewReader(resp.Body)
	// We'll use a simple approach: find the objects inside the array.
	// A better way is to use a JSON decoder and look for elements.
	dec := json.NewDecoder(reader)

	// Read the start of the array
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read start of array: %w", err)
	}
	if t != json.Delim('[') {
		return fmt.Errorf("expected [ at start of stream")
	}

	for dec.More() {
		var chunk struct {
			Candidates []struct {
				Content struct {
					Role  string `json:"role"`
					Parts []struct {
						Text     string `json:"text"`
						Call     *struct {
							Name string          `json:"name"`
							Args json.RawMessage `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata *struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}

		if err := dec.Decode(&chunk); err != nil {
			return fmt.Errorf("decode chunk: %w", err)
		}

		if len(chunk.Candidates) > 0 {
			cand := chunk.Candidates[0]
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					ch <- &Event{Type: EventTextDelta, Content: part.Text}
				}
				if part.Call != nil {
					// Gemini tool calls come in as functionCall objects
					ch <- &Event{
						Type: EventToolCall,
						ToolCall: &ToolCall{
							ID:   fmt.Sprintf("call_%d", time.Now().UnixNano()), // Gemini doesn't always provide an ID in v1beta?
							Name: part.Call.Name,
							Args: part.Call.Args,
						},
					}
				}
			}
		}

		if chunk.UsageMetadata != nil {
			ch <- &Event{
				Type: EventMessageEnd,
				Usage: &Usage{
					PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
					CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
				},
			}
		}
	}

	return nil
}

func (p *GoogleProvider) convertRequest(req *CompletionRequest) map[string]any {
	contents := []map[string]any{}
	for _, m := range req.Messages {
		parts := []map[string]any{}
		if m.Role == "tool" {
			parts = append(parts, map[string]any{
				"functionResponse": map[string]any{
					"name": m.ToolCallID, // Gemini uses name for tool_call_id
					"response": map[string]any{
						"content": m.Content,
					},
				},
			})
		} else if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Name,
						"args": tc.Args,
					},
				})
			}
		} else {
			parts = append(parts, map[string]any{"text": m.Content})
		}

		role := m.Role
		if role == "assistant" {
			role = "model"
		} else if role == "system" {
			// System prompt is handled separately in system_instruction
			continue
		} else if role == "success" {
			// Compaction summary — surface as a user turn so the model receives it.
			role = "user"
			parts = []map[string]any{{"text": "[Context Summary]\n" + m.Content}}
		}

		contents = append(contents, map[string]any{
			"role":  role,
			"parts": parts,
		})
	}

	body := map[string]any{
		"contents": contents,
	}

	if req.System != "" {
		body["system_instruction"] = map[string]any{
			"parts": []map[string]any{
				{"text": req.System},
			},
		}
	}

	if len(req.Tools) > 0 {
		tools := []map[string]any{}
		declarations := []map[string]any{}
		for _, t := range req.Tools {
			declarations = append(declarations, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  json.RawMessage(t.Schema),
			})
		}
		tools = append(tools, map[string]any{
			"function_declarations": declarations,
		})
		body["tools"] = tools
	}

	config := map[string]any{}
	if n := req.MaxTokens; n > 0 {
		config["max_output_tokens"] = n
	}
	if t := req.Temperature; t > 0 {
		config["temperature"] = t
	}
	if len(config) > 0 {
		body["generationConfig"] = config
	}

	return body
}

func (p *GoogleProvider) ListModels() ([]string, error) {
	url := p.baseURL + "/v1beta/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", p.apiKey)
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
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var names []string
	for _, m := range data.Models {
		// Names come as "models/gemini-..."
		names = append(names, strings.TrimPrefix(m.Name, "models/"))
	}
	return names, nil
}

var _ Provider = (*GoogleProvider)(nil)
var _ ModelLister = (*GoogleProvider)(nil)
