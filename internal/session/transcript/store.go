package transcript

import (
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/genai-io/gen-code/internal/log"
	"github.com/genai-io/gen-code/internal/task/tracker"
)

type StartCommand struct {
	SessionID string
	ProjectID string
	Cwd       string
	Provider  string
	Model     string
	ParentID  string
	Time      time.Time
}

type AppendMessageCommand struct {
	SessionID   string
	MessageID   string
	ParentID    string
	Time        time.Time
	Cwd         string
	GitBranch   string
	AgentID     string
	IsSidechain bool
	Role        string
	Content     []ContentBlock
}

type PatchStateCommand struct {
	SessionID string
	Time      time.Time
	Ops       []PatchOp
}

type CompactCommand struct {
	SessionID  string
	Time       time.Time
	BoundaryID string
}

type ForkCommand struct {
	SourceSessionID string
	NewSessionID    string
	Time            time.Time
}

type ListOptions struct {
	Limit            int
	IncludeSidechain bool
}

func PatchTitle(title string) PatchOp       { return mustPatch(PatchPathTitle, title) }
func PatchLastPrompt(prompt string) PatchOp { return mustPatch(PatchPathLastPrompt, prompt) }
func PatchTag(tag string) PatchOp           { return mustPatch(PatchPathTag, tag) }
func PatchMode(mode string) PatchOp         { return mustPatch(PatchPathMode, mode) }
func PatchTasks(tasks []tracker.Task) PatchOp {
	return mustPatch(PatchPathTasks, tasks)
}
func PatchWorktree(worktree *WorktreeState) PatchOp { return mustPatch(PatchPathWorktree, worktree) }

// StateOpsFor builds the full set of patch ops for a projected state.
// Used by the session save path to express the current snapshot as a single
// patch record.
//
// All fields are emitted unconditionally. Under the append-only persistence
// path the projector applies last-wins across patch records, so a missing op
// would let prior values survive rather than clear them. Always emitting
// ensures that clearing a value (empty tasks, exited worktree) reflects on
// the next save. PatchTasks with an empty slice serializes to "[]" and
// PatchWorktree(nil) to "null" — both already round-trip correctly through
// the projector.
func StateOpsFor(state State) []PatchOp {
	return []PatchOp{
		PatchTitle(state.Title),
		PatchLastPrompt(state.LastPrompt),
		PatchTag(state.Tag),
		PatchMode(state.Mode),
		PatchTasks(TrackerTasksFromView(state.Tasks)),
		PatchWorktree(state.Worktree),
	}
}

func mustPatch(path string, v any) PatchOp {
	data, err := json.Marshal(v)
	if err != nil {
		// Log instead of panicking — the marshal input is always controlled
		// (strings, simple structs), so this should never happen in practice.
		log.Logger().Error("transcript: mustPatch marshal failed", zap.String("path", path), zap.Error(err))
		return PatchOp{Path: path, Value: []byte("null")}
	}
	return PatchOp{
		Path:  path,
		Value: data,
	}
}
