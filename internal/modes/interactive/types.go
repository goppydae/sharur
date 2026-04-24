package interactive

import (
	"context"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/goppydae/gollm/internal/agent"
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

// agentEventMsg wraps an agent.Event for delivery through bubbletea.
type agentEventMsg struct{ ev agent.Event }


// model holds the TUI state.
type model struct {
	width  int
	height int
	style  Style

	// Session management
	sessionMgr *session.Manager
	config     *config.Config

	// Agent integration
	ag          *agent.Agent
	ctx         context.Context
	cancel      context.CancelFunc
	eventCh           <-chan agent.Event

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
	picker Picker
	fileCache []string


	// Input
	input         textarea.Model
	promptHistory []string
	historyIndex  int
	draftInput    string

	// Modal overlay
	modal modalState

	// Bubbles components
	spinner     spinner.Model
	helper      help.Model
	progressBar progress.Model
	stopwatch   stopwatch.Model

	initialPrompt string
}

// Picker holds the state of a generic list-based picker overlay.
type Picker struct {
	Open    bool
	Kind    pickerType
	Query   string
	Items   []string
	Matches []string
	Cursor  int
	Page    int
}

type pickerType int

const (
	pickerTypeFile pickerType = iota
	pickerTypeSlash
	pickerTypeSession
	pickerTypeSkill
	pickerTypePrompt
)

// toolCallEntry stores details about a tool call with its execution status.
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
