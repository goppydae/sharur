package interactive

import (
	"context"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/themes"
)

// Layout constants.
const (
	headerHeight    = 0
	inputHeight     = 1 // minimum textarea height
	footerHeight    = 1 // footer text line
	borderOffset    = 0
	chatMargin      = 0
	separatorHeight = 2 // border above input + above footer

	msgWidthRatio = 0.8 // message boxes use 80% of chat width
)

// agentEventMsg wraps a pb.AgentEvent for delivery through bubbletea.
type agentEventMsg struct{ ev *pb.AgentEvent }


// model holds the TUI state.
type model struct {
	width  int
	height int
	style  Style

	// Session management
	sessionMgr *session.Manager
	config     *config.Config

	// gRPC client
	client    pb.AgentServiceClient
	sessionID string

	// Context / cancellation
	ctx         context.Context
	cancel      context.CancelFunc
	eventCh     chan *pb.AgentEvent

	// Agent state
	isRunning         bool
	newAssistantEntry bool
	toolCallsExpanded bool // true when all tool calls are fully expanded
	modelName         string
	provider          string
	thinking          string
	tokens            int
	contextWindow     int
	startTime         time.Time
	queuedSteer       int
	queuedFollowUp    int
	isCompacting      atomic.Bool

	// Model cycling (Ctrl+P)
	models     []string // cycling list from --models
	modelIndex int      // current index in models list

	// Conversation history
	history []historyEntry

	// Cached viewport content
	chatContent string

	// Viewport (scrollable chat area)
	vp           viewport.Model
	userScrolled bool

	// Picker state
	picker          list.Model
	pickerOpen      bool
	lastPickerType  pickerType
	lastPickerQuery string


	// Input
	input         textarea.Model
	promptHistory []string
	historyIndex  int
	draftInput    string

	// Modal overlay
	modal modalState

	// Bubbles components
	keys        KeyMap
	spinner     spinner.Model
	helper      help.Model
	progressBar progress.Model
	stopwatch   stopwatch.Model

	initialPrompt string
}

type toolCallEntry struct {
	id              string
	name            string
	arg             string // first argument (path or command)
	status          toolCallStatus
	streamingOutput string // accumulated partial output
}

type toolCallStatus int

const (
	toolCallRunning toolCallStatus = iota
	toolCallSuccess
	toolCallFailure
)

// toolOutputEntry stores the result of a single tool execution.
type toolOutputEntry struct {
	toolCallID string
	toolName   string
	content    string
	isError    bool
}

// contentItemKind identifies the type of a content item in an ordered entry.
type contentItemKind int

const (
	contentItemThinking contentItemKind = iota
	contentItemText
	contentItemToolCall
	contentItemToolOutput
)

// contentItem is an ordered piece of an assistant message.
// Items preserve their temporal order from the LLM stream,
// enabling interleaved thinking ↔ tool call ↔ thinking patterns.
type contentItem struct {
	kind contentItemKind
	text string          // for thinking/text
	tc   toolCallEntry   // for toolCall
	out  toolOutputEntry // for toolOutput
}

// historyEntry is a single rendered message.
type historyEntry struct {
	role  string
	items []contentItem // ordered content items (replaces separate fields)
}

// Theme is an alias for themes.Theme.
type Theme = themes.Theme

// Style is an alias for themes.Style.
type Style = themes.Style

type initialPromptMsg struct{}

// syncHistoryMsg carries updated messages back to the model.
type syncHistoryMsg struct {
	messages []*pb.ConversationMessage
	err      error
}

// syncStateMsg carries updated session state back to the model.
type syncStateMsg struct {
	state *pb.GetStateResponse
	err   error
}

// errorMsg carries an error back to the model.
type errorMsg struct {
	err error
}

// sessionSwitchMsg carries a new session ID back to the model.
type sessionSwitchMsg struct {
	sessionID  string
	initialMsg *historyEntry
}

// compactDoneMsg is sent when the manual compaction RPC completes.
type compactDoneMsg struct {
	err error
}
