package transcript

import (
	"encoding/json"
	"time"
)

// Record type values follow <entity>.<verb> (past tense), lowercase,
// dot-separated. See docs/tracing.md for the full taxonomy.
const (
	SessionStarted   = "session.started"
	SessionForked    = "session.forked"
	SessionCompacted = "session.compacted"
	MessageAppended  = "message.appended"
	StatePatched     = "state.patched"
)

const (
	PatchPathTitle      = "title"
	PatchPathLastPrompt = "lastPrompt"
	PatchPathTag        = "tag"
	PatchPathMode       = "mode"
	PatchPathTasks      = "tasks"
	PatchPathWorktree   = "worktree"
)

type Record struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`

	ParentID    string `json:"parentId,omitempty"`
	IsSidechain bool   `json:"isSidechain,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Version     string `json:"version,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
	AgentID     string `json:"agentId,omitempty"`

	Message *MessageRecord `json:"message,omitempty"`
	State   *StateRecord   `json:"state,omitempty"`
	Session *SessionRecord `json:"session,omitempty"`
}

type MessageRecord struct {
	MessageID string         `json:"messageId"`
	Role      string         `json:"role"`
	Content   []ContentBlock `json:"content"`
}

type StateRecord struct {
	Ops []PatchOp `json:"ops"`
}

type PatchOp struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// SessionRecord carries the lifecycle payload for session.started /
// session.forked / session.compacted records. The three event types
// multiplex on a single struct because the fields are sparse and the
// projector dispatches on Record.Type rather than payload shape.
type SessionRecord struct {
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	ParentID   string `json:"parentId,omitempty"`
	BoundaryID string `json:"boundaryId,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   []ContentBlock `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`

	// Source marks the provenance of injected content (e.g. "hook:UserPromptSubmit",
	// "command:/identity", "reminder:system-reminder"). Empty for user-authored
	// blocks and for ContentBlocks that the model itself produced.
	Source string `json:"source,omitempty"`

	// ImageSource is the inlined image data on type=image blocks.
	ImageSource *ImageSource `json:"imageSource,omitempty"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type WorktreeState struct {
	OriginalCwd    string `json:"originalCwd"`
	WorktreePath   string `json:"worktreePath"`
	WorktreeName   string `json:"worktreeName"`
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
	OriginalBranch string `json:"originalBranch,omitempty"`
	Exited         bool   `json:"exited,omitempty"`
}
