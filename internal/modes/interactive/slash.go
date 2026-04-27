package interactive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goppydae/gollm/internal/config"
	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/prompts"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/skills"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// slashCommand represents a parsed slash command.
type slashCommand struct {
	name string
	arg  string // remainder after command name
	raw  string // full command string (including /)
}

// parseSlashCommand parses a leading /command [args] from text.
// Returns nil if the text doesn't start with a slash command.
func parseSlashCommand(text string) *slashCommand {
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	rest := text[1:]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return &slashCommand{name: rest, arg: "", raw: text}
	}
	return &slashCommand{
		name: rest[:spaceIdx],
		arg:  strings.TrimSpace(rest[spaceIdx+1:]),
		raw:  text,
	}
}

// AvailableSlashCommands lists all known slash commands for autocomplete.
var BaseSlashCommands = []string{"new", "resume", "branch", "fork", "rebase", "merge", "tree", "import", "export", "model", "stats", "compact", "config", "context", "exit", "quit"}

// knownCommand returns true if name is a recognized slash command.
func knownCommand(name string) bool {
	if strings.HasPrefix(name, "skill:") || strings.HasPrefix(name, "prompt:") {
		return true
	}
	for _, c := range BaseSlashCommands {
		if c == name {
			return true
		}
	}
	return false
}

// slashResult holds the result of processing a slash command.
type slashResult struct {
	historyEntry   historyEntry
	modalKind      modalKind
	modalNodes     []session.FlatNode
	modalMessages  []rebaseItem // for modalRebase
	compact        bool
	syncHistory    bool
	quit           bool
	expandInput    string
	sendDirectly   string
	invokeTool     string
	invokeToolArgs string
	// newSessionID is set when the active session should change.
	newSessionID string
}

// handleSlashCommand processes a slash command and returns the result.
// sessionID is a pointer so session-switching commands can update the model's active ID.
func handleSlashCommand(cmd *slashCommand, client pb.AgentServiceClient, sessionID *string, mgr *session.Manager, cfg *config.Config) (*slashResult, error) {
	switch {
	case cmd.name == "new":
		return handleNewSession(client, sessionID)

	case cmd.name == "resume":
		return handleResumeSession(client, sessionID, mgr, cmd.arg)

	case cmd.name == "branch":
		return handleBranchSession(client, sessionID, cmd.arg)

	case cmd.name == "fork":
		return handleForkSession(client, sessionID)

	case cmd.name == "rebase":
		return handleRebaseSession(client, sessionID)

	case cmd.name == "merge":
		return handleMergeSession(client, sessionID, mgr, cmd.arg)

	case cmd.name == "tree":
		return handleTreeCommand(client, mgr, *sessionID, cmd.arg)

	case cmd.name == "import":
		return handleImportSession(client, sessionID, mgr, cmd.arg)

	case cmd.name == "export":
		return handleExportSession(client, *sessionID, mgr, cmd.arg)

	case cmd.name == "model":
		return handleModelCommand(client, *sessionID, cfg, cmd.arg)

	case cmd.name == "stats":
		return handleStatsCommand(client, *sessionID)

	case cmd.name == "compact":
		return handleCompact(client, *sessionID)

	case cmd.name == "config":
		return handleConfigCommand(cfg)

	case cmd.name == "context":
		return handleContextCommand(client, *sessionID)

	case cmd.name == "skill":
		return handleSkillCommand(cfg, cmd.arg, "")

	case cmd.name == "prompt":
		return handlePromptCommand(cfg, cmd.arg)

	case strings.HasPrefix(cmd.name, "skill:"):
		skillName := strings.TrimPrefix(cmd.name, "skill:")
		return handleSkillCommand(cfg, skillName, cmd.arg)

	case strings.HasPrefix(cmd.name, "prompt:"):
		promptName := strings.TrimPrefix(cmd.name, "prompt:")
		return handlePromptCommand(cfg, promptName)

	case cmd.name == "exit" || cmd.name == "quit":
		return &slashResult{quit: true}, nil

	default:
		if res, err := handleSkillCommand(cfg, cmd.name, cmd.arg); err == nil && res.sendDirectly != "" {
			return res, nil
		}
		if res, err := handlePromptCommand(cfg, cmd.name); err == nil && res.expandInput != "" {
			return res, nil
		}
		return &slashResult{
			historyEntry: historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Unknown command: /%s", cmd.name)}}},
		}, nil
	}
}

func handleNewSession(client pb.AgentServiceClient, sessionID *string) (*slashResult, error) {
	resp, err := client.NewSession(context.Background(), &pb.NewSessionRequest{})
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	*sessionID = resp.SessionId
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("New session: %s", resp.SessionId)}}},
		newSessionID: resp.SessionId,
		syncHistory:  true,
	}, nil
}

func handleResumeSession(client pb.AgentServiceClient, sessionID *string, mgr *session.Manager, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /resume <session-id-or-path>"}}},
		}, nil
	}

	// Try to load via manager to verify it exists; if it's a path resolve to ID.
	id := arg
	if sess, err := mgr.Load(arg); err == nil {
		id = sess.ID
	} else if abs, err2 := filepath.Abs(arg); err2 == nil {
		if sess, err3 := mgr.Load(abs); err3 == nil {
			id = sess.ID
		}
	}

	*sessionID = id
	// The service will auto-load the session from disk on first use.
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Resumed session: %s", id)}}},
		newSessionID: id,
		syncHistory:  true,
	}, nil
}

// handleBranchSession creates a child session. arg is an optional message index (-1 = end).
func handleBranchSession(client pb.AgentServiceClient, sessionID *string, arg string) (*slashResult, error) {
	msgIndex := int32(-1)
	if arg != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil {
			msgIndex = int32(n)
		}
	}
	resp, err := client.BranchSession(context.Background(), &pb.BranchSessionRequest{
		SessionId:    *sessionID,
		MessageIndex: msgIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("branch session: %w", err)
	}
	parentID := *sessionID
	*sessionID = resp.SessionId
	label := fmt.Sprintf("Branched into new session: %s (parent: %s)", resp.SessionId, parentID)
	if msgIndex >= 0 {
		label = fmt.Sprintf("Branched at msg #%d into new session: %s (parent: %s)", msgIndex+1, resp.SessionId, parentID)
	}
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: label}}},
		newSessionID: resp.SessionId,
		syncHistory:  true,
	}, nil
}

// handleForkSession creates an independent snapshot of the current session (no parent link).
func handleForkSession(client pb.AgentServiceClient, sessionID *string) (*slashResult, error) {
	resp, err := client.ForkSession(context.Background(), &pb.ForkSessionRequest{SessionId: *sessionID})
	if err != nil {
		return nil, fmt.Errorf("fork session: %w", err)
	}
	*sessionID = resp.SessionId
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Forked into independent session: %s", resp.SessionId)}}},
		newSessionID: resp.SessionId,
		syncHistory:  true,
	}, nil
}

// handleRebaseSession fetches the current session messages and opens the rebase picker modal.
func handleRebaseSession(client pb.AgentServiceClient, sessionID *string) (*slashResult, error) {
	resp, err := client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: *sessionID})
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return &slashResult{
		modalKind:     modalRebase,
		modalMessages: buildRebaseItemsFromMsgs(resp.Messages),
	}, nil
}

// handleMergeSession calls MergeSession with the current session and the specified target.
func handleMergeSession(client pb.AgentServiceClient, sessionID *string, mgr *session.Manager, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /merge <session-id>"}}},
		}, nil
	}
	targetID, err := resolveSessionID(mgr, arg)
	if err != nil {
		return &slashResult{
			historyEntry: historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}},
		}, nil
	}
	resp, err := client.MergeSession(context.Background(), &pb.MergeSessionRequest{
		SessionIdA: *sessionID,
		SessionIdB: targetID,
	})
	if err != nil {
		return nil, fmt.Errorf("merge session: %w", err)
	}
	shortA := (*sessionID)[:min8(len(*sessionID))]
	shortB := targetID[:min8(len(targetID))]
	*sessionID = resp.SessionId
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Merged %s + %s → %s", shortA, shortB, resp.SessionId)}}},
		newSessionID: resp.SessionId,
		syncHistory:  true,
	}, nil
}

// resolveSessionID resolves a partial session ID or name to a full session ID.
// Resolution order: exact match → short prefix (≥4 chars) → name substring.
func resolveSessionID(mgr *session.Manager, input string) (string, error) {
	summaries, err := mgr.ListSummaries()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	input = strings.TrimSpace(input)
	inputLower := strings.ToLower(input)

	var matches []string
	for _, s := range summaries {
		if s.ID == input {
			return s.ID, nil // exact match
		}
		if len(input) >= 4 && strings.HasPrefix(strings.ToLower(s.ID), inputLower) {
			matches = append(matches, s.ID)
		} else if strings.Contains(strings.ToLower(s.Name), inputLower) {
			matches = append(matches, s.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous session reference %q: %d matches", input, len(matches))
	}
	return "", fmt.Errorf("session %q not found", input)
}

func min8(n int) int {
	if n < 8 {
		return n
	}
	return 8
}

func handleTreeCommand(client pb.AgentServiceClient, mgr *session.Manager, currentID string, arg string) (*slashResult, error) {
	scope := pb.GetSessionTreeRequest_SESSION
	mscope := session.ScopeSession
	if strings.Contains(arg, "--global") || strings.Contains(arg, "-g") {
		scope = pb.GetSessionTreeRequest_GLOBAL
		mscope = session.ScopeGlobal
	} else if strings.Contains(arg, "--project") || strings.Contains(arg, "-p") {
		scope = pb.GetSessionTreeRequest_PROJECT
		mscope = session.ScopeProject
	}

	// Prefer the gRPC session tree; fall back to local manager tree.
	treeResp, err := client.GetSessionTree(context.Background(), &pb.GetSessionTreeRequest{
		SessionId: currentID,
		Scope:     scope,
	})
	if err == nil {
		nodes := buildFlatProtoNodes(treeResp.Roots)
		if len(nodes) == 0 {
			return &slashResult{
				historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: "No conversation history found in this workspace yet."}}},
			}, nil
		}
		return &slashResult{
			modalKind:  modalTree,
			modalNodes: nodes,
		}, nil
	}
	// Fallback: build from disk.
	roots, berr := mgr.BuildTree(currentID, mscope)
	if berr != nil {
		return nil, berr
	}

	nodes := session.FlattenTree(roots)
	if len(nodes) == 0 {
		return &slashResult{
			historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: "No conversation history found in this workspace yet."}}},
		}, nil
	}

	return &slashResult{
		modalKind:  modalTree,
		modalNodes: nodes,
	}, nil
}

// buildFlatProtoNodes mirrors session.FlattenTree for proto nodes, including
// multi-root connector logic so sibling root sessions show ├─ / └─ lines.
func buildFlatProtoNodes(roots []*pb.SessionNode) []session.FlatNode {
	var result []session.FlatNode
	multipleRoots := len(roots) > 1

	var walk func(n *pb.SessionNode, indent int, justBranched bool, isLast bool, gutters []session.GutterInfo)
	walk = func(n *pb.SessionNode, indent int, justBranched bool, isLast bool, gutters []session.GutterInfo) {
		// showConnector if parent branched
		showConnector := justBranched

		result = append(result, session.FlatNode{
			Node:          protoToTreeNode(n),
			Indent:        indent,
			ShowConnector: showConnector,
			IsLast:        isLast,
			Gutters:       gutters,
		})

		children := n.Children
		multipleChildren := len(children) > 1

		childIndent := indent
		if multipleChildren {
			childIndent = indent + 1
		} else if justBranched && indent > 0 {
			childIndent = indent + 1
		}

		for i, child := range children {
			childIsLast := i == len(children)-1
			childGutters := append([]session.GutterInfo{}, gutters...)

			if showConnector {
				pos := indent - 1
				if pos >= 0 {
					childGutters = append(childGutters, session.GutterInfo{Position: pos, Show: !isLast})
				}
			}

			walk(child, childIndent, multipleChildren, childIsLast, childGutters)
		}
	}

	for i, root := range roots {
		indent := 0
		if multipleRoots {
			indent = 1
		}
		var initialGutters []session.GutterInfo
		if multipleRoots && i < len(roots)-1 {
			initialGutters = []session.GutterInfo{{Position: 0, Show: true}}
		}
		walk(root, indent, multipleRoots, i == len(roots)-1, initialGutters)
	}
	return result
}

func protoToTreeNode(n *pb.SessionNode) *session.TreeNode {
	node := &session.TreeNode{
		ID:           n.SessionId,
		Name:         n.Name,
		FirstMessage: n.FirstMessage,
		CreatedAt:    time.Unix(n.CreatedAt, 0),
		UpdatedAt:    time.Unix(n.UpdatedAt, 0),
		Role:         n.Role,
		Content:      n.Content,
		IsActive:     n.IsActive,
	}
	if n.ParentMessageIndex >= 0 {
		idx := int(n.ParentMessageIndex)
		node.ParentMessageIndex = &idx
	}
	if n.MergeSourceId != "" {
		id := n.MergeSourceId
		node.MergeSourceID = &id
	}
	return node
}

// buildRebaseItemsFromMsgs converts proto messages to rebaseItem slice for the picker.
func buildRebaseItemsFromMsgs(msgs []*pb.ConversationMessage) []rebaseItem {
	items := make([]rebaseItem, len(msgs))
	for i, m := range msgs {
		content := m.Content
		if len(content) > 72 {
			content = content[:72] + "…"
		}
		items[i] = rebaseItem{index: i, role: m.Role, content: content, checked: true}
	}
	return items
}

func handleImportSession(client pb.AgentServiceClient, sessionID *string, mgr *session.Manager, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /import <path-to-jsonl>"}}},
		}, nil
	}

	var sess *session.Session
	var err error
	if strings.Contains(arg, string(os.PathSeparator)) || strings.HasSuffix(arg, ".jsonl") {
		sess, err = mgr.LoadPath(arg)
	} else {
		sess, err = mgr.Load(arg)
	}
	if err != nil {
		return nil, fmt.Errorf("import session: %w", err)
	}

	// Create a new session on the service side.
	newResp, err := client.NewSession(context.Background(), &pb.NewSessionRequest{})
	if err != nil {
		return nil, fmt.Errorf("create session for import: %w", err)
	}
	newID := newResp.SessionId

	// Configure the session with imported metadata.
	_, _ = client.ConfigureSession(context.Background(), &pb.ConfigureSessionRequest{
		SessionId:     newID,
		SystemPrompt:  ptr(sess.SystemPrompt),
		ThinkingLevel: ptr(sess.Thinking),
	})

	// Save the imported session file under the new ID so the service can load it.
	importedSess := &session.Session{
		ID:           newID,
		Name:         "Imported: " + sess.Name,
		Model:        sess.Model,
		Provider:     sess.Provider,
		Thinking:     sess.Thinking,
		SystemPrompt: sess.SystemPrompt,
		Messages:     sess.Messages,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	_ = mgr.Save(importedSess)

	*sessionID = newID
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Imported session: %s (%d messages)", newID, len(sess.Messages))}}},
		newSessionID: newID,
		syncHistory:  true,
	}, nil
}

func handleExportSession(client pb.AgentServiceClient, sessionID string, mgr *session.Manager, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /export <path-to-destination.jsonl>"}}},
		}, nil
	}

	state, err := client.GetState(context.Background(), &pb.GetStateRequest{SessionId: sessionID})
	if err != nil {
		return nil, fmt.Errorf("get session state: %w", err)
	}
	msgs, err := client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: sessionID})
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	typesMsgs := make([]types.Message, 0, len(msgs.Messages))
	for _, m := range msgs.Messages {
		msg := types.Message{
			Role:       m.Role,
			Content:    m.Content,
			Thinking:   m.Thinking,
			ToolCallID: m.ToolCallId,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
				ID:   tc.Id,
				Name: tc.Name,
				Args: json.RawMessage(tc.ArgsJson),
			})
		}
		typesMsgs = append(typesMsgs, msg)
	}

	sessToSave := &session.Session{
		ID:           sessionID,
		Model:        state.Model,
		Provider:     state.Provider,
		Thinking:     state.ThinkingLevel,
		SystemPrompt: state.SystemPrompt,
		Messages:     typesMsgs,
		UpdatedAt:    time.Now(),
		CreatedAt:    time.Now(),
	}

	if err := mgr.SavePath(sessToSave, arg); err != nil {
		return nil, fmt.Errorf("export session: %w", err)
	}

	return &slashResult{
		historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Exported session to: %s", arg)}}},
	}, nil
}

func handleStatsCommand(client pb.AgentServiceClient, sessionID string) (*slashResult, error) {
	return &slashResult{modalKind: modalStats}, nil
}

func handleCompact(client pb.AgentServiceClient, sessionID string) (*slashResult, error) {
	return &slashResult{compact: true}, nil
}

func handleConfigCommand(cfg *config.Config) (*slashResult, error) {
	return &slashResult{modalKind: modalConfig}, nil
}

func handleContextCommand(client pb.AgentServiceClient, sessionID string) (*slashResult, error) {
	state, err := client.GetState(context.Background(), &pb.GetStateRequest{SessionId: sessionID})
	msgs, _ := client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: sessionID})
	if err != nil {
		return nil, err
	}
	var contextTokens int32
	var userMsgs, assistantMsgs, toolResults, toolCalls int
	if msgs != nil {
		for _, m := range msgs.Messages {
			switch m.Role {
			case "user":
				userMsgs++
			case "assistant":
				assistantMsgs++
				toolCalls += len(m.ToolCalls)
			case "tool":
				toolResults++
			}
		}
	}
	_ = contextTokens
	usageTxt := fmt.Sprintf("Context usage: %d messages (%d user, %d assistant, %d tool results, %d tool calls)",
		userMsgs+assistantMsgs+toolResults, userMsgs, assistantMsgs, toolResults, toolCalls)
	if state.ProviderInfo != nil && state.ProviderInfo.ContextWindow > 0 {
		usageTxt += fmt.Sprintf(" — window: %d tokens", state.ProviderInfo.ContextWindow)
	}
	return &slashResult{
		historyEntry: historyEntry{
			role:  "info",
			items: []contentItem{{kind: contentItemText, text: usageTxt}},
		},
	}, nil
}

func handleModelCommand(client pb.AgentServiceClient, sessionID string, cfg *config.Config, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /model <provider/model> (e.g. /model anthropic/claude-opus-4-5)"}}},
		}, nil
	}

	providerName := cfg.Provider
	modelName := arg
	if idx := strings.IndexByte(arg, '/'); idx >= 0 {
		providerName = arg[:idx]
		modelName = arg[idx+1:]
	}

	req := &pb.ConfigureSessionRequest{
		SessionId: sessionID,
		Model:     ptr(modelName),
	}
	if providerName != cfg.Provider {
		req.Provider = ptr(providerName)
	}
	_, err := client.ConfigureSession(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("switch model: %w", err)
	}

	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Switched to %s/%s", providerName, modelName)}}},
		syncHistory:  true,
	}, nil
}

func handleSkillCommand(cfg *config.Config, skillName, args string) (*slashResult, error) {
	if skillName == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /skill:<name> [args]"}}},
		}, nil
	}

	skillDirs := append([]string{}, cfg.Extensions...)
	skillDirs = append(skillDirs, skills.DefaultDirs()...)
	skillDirs = append(skillDirs, cfg.SkillPaths...)

	allSkills, err := skills.Discover(skillDirs...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover skills: %w", err)
	}

	var skill *skills.Skill
	for _, s := range allSkills {
		if s.Name == skillName {
			skill = s
			break
		}
	}

	if skill == nil {
		return nil, fmt.Errorf("skill %q not found", skillName)
	}

	argBytes, _ := json.Marshal(args)
	return &slashResult{
		invokeTool:     "skill_" + skill.Name,
		invokeToolArgs: fmt.Sprintf(`{"args":%s}`, string(argBytes)),
	}, nil
}

func handlePromptCommand(cfg *config.Config, promptName string) (*slashResult, error) {
	if promptName == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /prompt:<name>"}}},
		}, nil
	}

	promptDirs := append([]string{}, prompts.DefaultDirs()...)
	promptDirs = append(promptDirs, cfg.PromptTemplatePaths...)

	allPrompts, err := prompts.Discover(promptDirs...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover prompt templates: %w", err)
	}

	var content string
	for _, p := range allPrompts {
		name := strings.TrimSuffix(filepath.Base(p.Path), ".md")
		if name == promptName {
			content = p.Template
			break
		}
	}

	if content == "" {
		return nil, fmt.Errorf("prompt template %q not found", promptName)
	}

	return &slashResult{expandInput: content}, nil
}

// BangCommandResult holds the result of a !command execution.
type BangCommandResult struct {
	Output  string
	IsError bool
}

// HandleBangCommand executes a shell command from a "!cmd" or "!!cmd" input.
func HandleBangCommand(raw string) (result BangCommandResult, sendDirectly bool) {
	if strings.HasPrefix(raw, "!!") {
		sendDirectly = true
		raw = raw[2:]
	} else {
		raw = raw[1:]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}

	argsJSON := []byte(`{"command":` + jsonQuote(raw) + `}`)
	t := tools.Bash{}
	res, execErr := t.Execute(context.Background(), argsJSON, nil)
	if execErr != nil {
		result.Output = "Error: " + execErr.Error()
		result.IsError = true
		return
	}
	result.Output = res.Content
	result.IsError = res.IsError
	return
}

func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

func ptr[T any](v T) *T { return &v }
