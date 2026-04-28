package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	proto "github.com/goppydae/gollm/extensions/gen"
	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

const extensionRPCTimeout = 5 * time.Second

// Serve starts a gRPC server on the Unix socket path provided via GOLLM_SOCKET_PATH.
// This is the entry point for extension binaries.
func Serve(impl Plugin) {
	socketPath := os.Getenv("GOLLM_SOCKET_PATH")
	if socketPath == "" {
		log.Fatal("extensions.Serve: GOLLM_SOCKET_PATH environment variable not set")
	}
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("extensions.Serve: listen %s: %v", socketPath, err)
	}
	s := grpc.NewServer()
	proto.RegisterExtensionServer(s, &GRPCServer{Impl: impl})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("extensions.Serve: serve: %v", err)
	}
}

// GRPCClient is an implementation of agent.Extension that talks over RPC.
// It runs on the host side when a plugin binary is loaded.
//
// If Name() or Tools() fail, the client is marked degraded and all subsequent
// tool executions return an error rather than silently doing nothing.
type GRPCClient struct {
	client      proto.ExtensionClient
	degraded    bool
	degradedErr error
}

// Degraded reports whether the extension failed to initialise. Callers can
// surface this to the user rather than letting the failure be silent.
func (m *GRPCClient) Degraded() (bool, error) {
	return m.degraded, m.degradedErr
}

func (m *GRPCClient) Name() string {
	ctx, cancel := context.WithTimeout(context.Background(), extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.Name(ctx, &proto.Empty{})
	if err != nil {
		log.Printf("extension Name() RPC error (marking degraded): %v", err)
		m.degraded = true
		m.degradedErr = fmt.Errorf("Name() RPC failed: %w", err)
		return ""
	}
	return resp.Name
}

// Tools queries the extension process for its tool definitions and returns
// RemoteTool wrappers that execute each tool over the ExecuteTool RPC.
func (m *GRPCClient) Tools() []tools.Tool {
	ctx, cancel := context.WithTimeout(context.Background(), extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.Tools(ctx, &proto.Empty{})
	if err != nil {
		log.Printf("extension Tools() RPC error (marking degraded): %v", err)
		m.degraded = true
		m.degradedErr = fmt.Errorf("Tools() RPC failed: %w", err)
		return nil
	}
	result := make([]tools.Tool, 0, len(resp.Tools))
	for _, def := range resp.Tools {
		result = append(result, &RemoteTool{
			client:      m.client,
			name:        def.Name,
			description: def.Description,
			schema:      json.RawMessage(def.ParametersJsonSchema),
			readOnly:    def.IsReadOnly,
			extension:   m,
		})
	}
	return result
}

func (m *GRPCClient) BeforePrompt(ctx context.Context, state *agent.AgentState) *agent.AgentState {
	resp, err := m.client.BeforePrompt(ctx, &proto.BeforePromptRequest{
		State: &proto.AgentState{
			Prompt:        state.SystemPrompt,
			Model:         state.Model,
			Provider:      state.Provider,
			ThinkingLevel: string(state.Thinking),
		},
	})
	if err != nil {
		log.Printf("extension BeforePrompt() RPC error: %v", err)
		return state
	}
	if resp.State == nil {
		return state
	}
	newState := *state
	newState.SystemPrompt = resp.State.Prompt
	if resp.State.Model != "" {
		newState.Model = resp.State.Model
	}
	if resp.State.Provider != "" {
		newState.Provider = resp.State.Provider
	}
	if resp.State.ThinkingLevel != "" {
		newState.Thinking = agent.ThinkingLevel(resp.State.ThinkingLevel)
	}
	return &newState
}

func (m *GRPCClient) BeforeToolCall(ctx context.Context, call *agent.ToolCall, args json.RawMessage) (*tools.ToolResult, bool) {
	argsJSON := ""
	if args != nil {
		argsJSON = string(args)
	}
	resp, err := m.client.BeforeToolCall(ctx, &proto.BeforeToolCallRequest{
		Call: &proto.ToolCall{Name: call.Name, ArgumentsJson: argsJSON},
	})
	if err != nil {
		log.Printf("extension BeforeToolCall() RPC error: %v", err)
		return nil, false
	}
	if !resp.Intercepted {
		return nil, false
	}
	if resp.Result == nil {
		return &tools.ToolResult{}, true
	}
	if resp.Result.Error != "" {
		return &tools.ToolResult{Content: resp.Result.Error, IsError: true}, true
	}
	return &tools.ToolResult{Content: resp.Result.Output}, true
}

func (m *GRPCClient) AfterToolCall(ctx context.Context, call *agent.ToolCall, result *tools.ToolResult) *tools.ToolResult {
	argsJSON := ""
	if call.Args != nil {
		argsJSON = string(call.Args)
	}
	protoResult := &proto.ToolResult{
		Output: result.Content,
	}
	if result.IsError {
		protoResult.Error = result.Content
		protoResult.Output = ""
	}

	resp, err := m.client.AfterToolCall(ctx, &proto.AfterToolCallRequest{
		Call: &proto.ToolCall{
			Name:          call.Name,
			ArgumentsJson: argsJSON,
		},
		Result: protoResult,
	})
	if err != nil {
		log.Printf("extension AfterToolCall() RPC error: %v", err)
		return result
	}
	if resp.Result == nil {
		return result
	}
	if resp.Result.Error != "" {
		return &tools.ToolResult{Content: resp.Result.Error, IsError: true}
	}
	return &tools.ToolResult{Content: resp.Result.Output}
}

func (m *GRPCClient) ModifySystemPrompt(prompt string) string {
	resp, err := m.client.ModifySystemPrompt(context.Background(), &proto.ModifySystemPromptRequest{
		CurrentPrompt: prompt,
	})
	if err != nil {
		log.Printf("extension ModifySystemPrompt() RPC error: %v", err)
		return prompt
	}
	return resp.ModifiedPrompt
}

func (m *GRPCClient) SessionStart(ctx context.Context, sessionID string, reason agent.SessionStartReason) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.SessionStart(rpcCtx, &proto.SessionStartRequest{SessionId: sessionID, Reason: string(reason)}); err != nil {
		log.Printf("extension SessionStart() RPC error: %v", err)
	}
}

func (m *GRPCClient) SessionEnd(ctx context.Context, sessionID string, reason agent.SessionEndReason) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.SessionEnd(rpcCtx, &proto.SessionEndRequest{SessionId: sessionID, Reason: string(reason)}); err != nil {
		log.Printf("extension SessionEnd() RPC error: %v", err)
	}
}

func (m *GRPCClient) AgentStart(ctx context.Context) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.AgentStart(rpcCtx, &proto.Empty{}); err != nil {
		log.Printf("extension AgentStart() RPC error: %v", err)
	}
}

func (m *GRPCClient) AgentEnd(ctx context.Context) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.AgentEnd(rpcCtx, &proto.Empty{}); err != nil {
		log.Printf("extension AgentEnd() RPC error: %v", err)
	}
}

func (m *GRPCClient) TurnStart(ctx context.Context) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.TurnStart(rpcCtx, &proto.Empty{}); err != nil {
		log.Printf("extension TurnStart() RPC error: %v", err)
	}
}

func (m *GRPCClient) TurnEnd(ctx context.Context) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.TurnEnd(rpcCtx, &proto.Empty{}); err != nil {
		log.Printf("extension TurnEnd() RPC error: %v", err)
	}
}

func (m *GRPCClient) ModifyInput(ctx context.Context, text string) agent.InputResult {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.ModifyInput(rpcCtx, &proto.ModifyInputRequest{Text: text})
	if err != nil {
		log.Printf("extension ModifyInput() RPC error: %v", err)
		return agent.InputResult{Action: agent.InputContinue}
	}
	return agent.InputResult{
		Action: agent.InputAction(resp.Action),
		Text:   resp.Text,
	}
}

func (m *GRPCClient) ModifyContext(ctx context.Context, messages []types.Message) []types.Message {
	data, err := json.Marshal(messages)
	if err != nil {
		log.Printf("extension ModifyContext() marshal error: %v", err)
		return messages
	}
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.ModifyContext(rpcCtx, &proto.ModifyContextRequest{MessagesJson: string(data)})
	if err != nil {
		log.Printf("extension ModifyContext() RPC error: %v", err)
		return messages
	}
	var out []types.Message
	if err := json.Unmarshal([]byte(resp.MessagesJson), &out); err != nil {
		log.Printf("extension ModifyContext() unmarshal error: %v", err)
		return messages
	}
	return out
}

func (m *GRPCClient) BeforeProviderRequest(ctx context.Context, req *llm.CompletionRequest) *llm.CompletionRequest {
	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("extension BeforeProviderRequest() marshal error: %v", err)
		return req
	}
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.BeforeProviderRequest(rpcCtx, &proto.BeforeProviderRequestRequest{RequestJson: string(data)})
	if err != nil {
		log.Printf("extension BeforeProviderRequest() RPC error: %v", err)
		return req
	}
	var out llm.CompletionRequest
	if err := json.Unmarshal([]byte(resp.RequestJson), &out); err != nil {
		log.Printf("extension BeforeProviderRequest() unmarshal error: %v", err)
		return req
	}
	return &out
}

func (m *GRPCClient) AfterProviderResponse(ctx context.Context, content string, numToolCalls int) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.AfterProviderResponse(rpcCtx, &proto.AfterProviderResponseRequest{
		Content:      content,
		NumToolCalls: int32(numToolCalls),
	}); err != nil {
		log.Printf("extension AfterProviderResponse() RPC error: %v", err)
	}
}

func (m *GRPCClient) BeforeCompact(ctx context.Context, prep agent.CompactionPrep) *agent.CompactionResult {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	resp, err := m.client.BeforeCompact(rpcCtx, &proto.BeforeCompactRequest{
		MessageCount:    int32(prep.MessageCount),
		EstimatedTokens: int32(prep.EstimatedTokens),
		PreviousSummary: prep.PreviousSummary,
	})
	if err != nil {
		log.Printf("extension BeforeCompact() RPC error: %v", err)
		return nil
	}
	if !resp.Handled {
		return nil
	}
	return &agent.CompactionResult{
		Summary:          resp.Summary,
		FirstKeptEntryID: resp.FirstKeptEntryId,
	}
}

func (m *GRPCClient) AfterCompact(ctx context.Context, freedTokens int) {
	rpcCtx, cancel := context.WithTimeout(ctx, extensionRPCTimeout)
	defer cancel()
	if _, err := m.client.AfterCompact(rpcCtx, &proto.AfterCompactRequest{FreedTokens: int32(freedTokens)}); err != nil {
		log.Printf("extension AfterCompact() RPC error: %v", err)
	}
}

// RemoteTool is a tools.Tool that executes over the extension's ExecuteTool gRPC.
type RemoteTool struct {
	client      proto.ExtensionClient
	extension   *GRPCClient // back-ref to check degraded state
	name        string
	description string
	schema      json.RawMessage
	readOnly    bool
}

func (t *RemoteTool) Name() string            { return t.name }
func (t *RemoteTool) Description() string     { return t.description }
func (t *RemoteTool) Schema() json.RawMessage { return t.schema }
func (t *RemoteTool) IsReadOnly() bool        { return t.readOnly }

func (t *RemoteTool) Execute(ctx context.Context, args json.RawMessage, update tools.ToolUpdate) (*tools.ToolResult, error) {
	if t.extension != nil && t.extension.degraded {
		return &tools.ToolResult{
			Content: fmt.Sprintf("extension is degraded: %v", t.extension.degradedErr),
			IsError: true,
		}, nil
	}
	resp, err := t.client.ExecuteTool(ctx, &proto.ExecuteToolRequest{
		Name:          t.name,
		ArgumentsJson: string(args),
	})
	if err != nil {
		return nil, fmt.Errorf("remote tool %q: %w", t.name, err)
	}
	result := &tools.ToolResult{
		Content: resp.Content,
		IsError: resp.IsError,
	}
	if update != nil {
		update(result)
	}
	return result, nil
}

// GRPCServer is the gRPC server that runs inside the plugin binary.
// It adapts the Plugin interface to the proto service.
type GRPCServer struct {
	proto.UnimplementedExtensionServer
	Impl Plugin
}

func (m *GRPCServer) Name(ctx context.Context, _ *proto.Empty) (*proto.NameResponse, error) {
	return &proto.NameResponse{Name: m.Impl.Name()}, nil
}

func (m *GRPCServer) Tools(ctx context.Context, _ *proto.Empty) (*proto.ToolsResponse, error) {
	var defs []*proto.ToolDefinition
	for _, td := range m.Impl.Tools() {
		defs = append(defs, &proto.ToolDefinition{
			Name:                 td.Name,
			Description:          td.Description,
			ParametersJsonSchema: string(td.Schema),
			IsReadOnly:           td.IsReadOnly,
		})
	}
	return &proto.ToolsResponse{Tools: defs}, nil
}

func (m *GRPCServer) ExecuteTool(ctx context.Context, req *proto.ExecuteToolRequest) (*proto.ExecuteToolResponse, error) {
	result := m.Impl.ExecuteTool(ctx, req.Name, json.RawMessage(req.ArgumentsJson))
	return &proto.ExecuteToolResponse{Content: result.Content, IsError: result.IsError}, nil
}

func (m *GRPCServer) BeforePrompt(ctx context.Context, req *proto.BeforePromptRequest) (*proto.BeforePromptResponse, error) {
	state := AgentState{
		SystemPrompt:  req.State.Prompt,
		Model:         req.State.Model,
		Provider:      req.State.Provider,
		ThinkingLevel: req.State.ThinkingLevel,
	}
	modified := m.Impl.BeforePrompt(ctx, state)
	return &proto.BeforePromptResponse{
		State: &proto.AgentState{
			Prompt:        modified.SystemPrompt,
			Model:         modified.Model,
			Provider:      modified.Provider,
			ThinkingLevel: modified.ThinkingLevel,
		},
	}, nil
}

func (m *GRPCServer) AfterToolCall(ctx context.Context, req *proto.AfterToolCallRequest) (*proto.AfterToolCallResponse, error) {
	if req.Call == nil || req.Result == nil {
		return &proto.AfterToolCallResponse{Result: req.Result}, nil
	}
	call := ToolCall{Name: req.Call.Name, Args: json.RawMessage(req.Call.ArgumentsJson)}
	inResult := ToolResult{Content: req.Result.Output}
	if req.Result.Error != "" {
		inResult = ToolResult{Content: req.Result.Error, IsError: true}
	}
	out := m.Impl.AfterToolCall(ctx, call, inResult)
	protoResult := &proto.ToolResult{Output: out.Content}
	if out.IsError {
		protoResult.Error = out.Content
		protoResult.Output = ""
	}
	return &proto.AfterToolCallResponse{Result: protoResult}, nil
}

func (m *GRPCServer) BeforeToolCall(ctx context.Context, req *proto.BeforeToolCallRequest) (*proto.BeforeToolCallResponse, error) {
	if req.Call == nil {
		return &proto.BeforeToolCallResponse{}, nil
	}
	call := ToolCall{Name: req.Call.Name, Args: json.RawMessage(req.Call.ArgumentsJson)}
	result, intercepted := m.Impl.BeforeToolCall(ctx, call, json.RawMessage(req.Call.ArgumentsJson))
	if !intercepted {
		return &proto.BeforeToolCallResponse{Intercepted: false}, nil
	}
	protoResult := &proto.ToolResult{Output: result.Content}
	if result.IsError {
		protoResult.Error = result.Content
		protoResult.Output = ""
	}
	return &proto.BeforeToolCallResponse{Result: protoResult, Intercepted: true}, nil
}

func (m *GRPCServer) ModifySystemPrompt(ctx context.Context, req *proto.ModifySystemPromptRequest) (*proto.ModifySystemPromptResponse, error) {
	return &proto.ModifySystemPromptResponse{
		ModifiedPrompt: m.Impl.ModifySystemPrompt(req.CurrentPrompt),
	}, nil
}

func (m *GRPCServer) SessionStart(ctx context.Context, req *proto.SessionStartRequest) (*proto.Empty, error) {
	m.Impl.SessionStart(ctx, req.SessionId, agent.SessionStartReason(req.Reason))
	return &proto.Empty{}, nil
}

func (m *GRPCServer) SessionEnd(ctx context.Context, req *proto.SessionEndRequest) (*proto.Empty, error) {
	m.Impl.SessionEnd(ctx, req.SessionId, agent.SessionEndReason(req.Reason))
	return &proto.Empty{}, nil
}

func (m *GRPCServer) AgentStart(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	m.Impl.AgentStart(ctx)
	return &proto.Empty{}, nil
}

func (m *GRPCServer) AgentEnd(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	m.Impl.AgentEnd(ctx)
	return &proto.Empty{}, nil
}

func (m *GRPCServer) TurnStart(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	m.Impl.TurnStart(ctx)
	return &proto.Empty{}, nil
}

func (m *GRPCServer) TurnEnd(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	m.Impl.TurnEnd(ctx)
	return &proto.Empty{}, nil
}

func (m *GRPCServer) ModifyInput(ctx context.Context, req *proto.ModifyInputRequest) (*proto.ModifyInputResponse, error) {
	result := m.Impl.ModifyInput(ctx, req.Text)
	return &proto.ModifyInputResponse{Action: string(result.Action), Text: result.Text}, nil
}

func (m *GRPCServer) ModifyContext(ctx context.Context, req *proto.ModifyContextRequest) (*proto.ModifyContextResponse, error) {
	out := m.Impl.ModifyContext(ctx, req.MessagesJson)
	return &proto.ModifyContextResponse{MessagesJson: out}, nil
}

func (m *GRPCServer) BeforeProviderRequest(ctx context.Context, req *proto.BeforeProviderRequestRequest) (*proto.BeforeProviderRequestResponse, error) {
	out := m.Impl.BeforeProviderRequest(ctx, req.RequestJson)
	return &proto.BeforeProviderRequestResponse{RequestJson: out}, nil
}

func (m *GRPCServer) AfterProviderResponse(ctx context.Context, req *proto.AfterProviderResponseRequest) (*proto.Empty, error) {
	m.Impl.AfterProviderResponse(ctx, req.Content, int(req.NumToolCalls))
	return &proto.Empty{}, nil
}

func (m *GRPCServer) BeforeCompact(ctx context.Context, req *proto.BeforeCompactRequest) (*proto.BeforeCompactResponse, error) {
	prep := agent.CompactionPrep{
		MessageCount:    int(req.MessageCount),
		EstimatedTokens: int(req.EstimatedTokens),
		PreviousSummary: req.PreviousSummary,
	}
	result := m.Impl.BeforeCompact(ctx, prep)
	if result == nil {
		return &proto.BeforeCompactResponse{Handled: false}, nil
	}
	return &proto.BeforeCompactResponse{
		Handled:           true,
		Summary:           result.Summary,
		FirstKeptEntryId:  result.FirstKeptEntryID,
	}, nil
}

func (m *GRPCServer) AfterCompact(ctx context.Context, req *proto.AfterCompactRequest) (*proto.Empty, error) {
	m.Impl.AfterCompact(ctx, int(req.FreedTokens))
	return &proto.Empty{}, nil
}
