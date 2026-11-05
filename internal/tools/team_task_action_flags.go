package tools

import (
	"context"
	"sync"
)

// TaskActionFlags tracks which team_tasks actions a member agent took during a turn.
// The consumer reads these flags post-turn to decide whether to auto-complete,
// skip, or re-dispatch the task.
//
// No mutex needed for the bool fields: a single goroutine writes (agent loop),
// and the consumer reads only after the turn ends (sequential). Async
// delegation IDs use a mutex because parallel tool calls can append them.
type TaskActionFlags struct {
	Completed  bool // team_tasks(action="complete")
	Reviewed   bool // team_tasks(action="review")
	Escalated  bool // team_tasks(action="comment", type="blocker")
	Progressed bool // team_tasks(action="progress")
	Commented  bool // team_tasks(action="comment")
	Claimed    bool // team_tasks(action="claim")

	mu               sync.Mutex
	asyncDelegations []string
}

// HasAny returns true if the member interacted with the task system at all.
func (f *TaskActionFlags) HasAny() bool {
	return f.Completed || f.Reviewed || f.Escalated || f.Progressed || f.Commented || f.Claimed
}

func (f *TaskActionFlags) AddAsyncDelegation(id string) {
	if id == "" {
		return
	}
	f.mu.Lock()
	f.asyncDelegations = append(f.asyncDelegations, id)
	f.mu.Unlock()
}

func (f *TaskActionFlags) AsyncDelegations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asyncDelegations...)
}

const ctxTaskActionFlags toolContextKey = "tool_task_action_flags"

var sessionTaskActionFlags sync.Map

// WithTaskActionFlags injects a TaskActionFlags into context.
func WithTaskActionFlags(ctx context.Context, flags *TaskActionFlags) context.Context {
	return context.WithValue(ctx, ctxTaskActionFlags, flags)
}

// RegisterSessionTaskActionFlags exposes flags to tool calls that reach the
// gateway without the run context, such as MCP bridge calls from CLI-backed
// providers, which only carry the session key.
func RegisterSessionTaskActionFlags(sessionKey string, flags *TaskActionFlags) func() {
	if sessionKey == "" || flags == nil {
		return func() {}
	}
	sessionTaskActionFlags.Store(sessionKey, flags)
	return func() { sessionTaskActionFlags.CompareAndDelete(sessionKey, flags) }
}

// TaskActionFlagsFromCtx returns the TaskActionFlags from context, or nil.
func TaskActionFlagsFromCtx(ctx context.Context) *TaskActionFlags {
	if v, ok := ctx.Value(ctxTaskActionFlags).(*TaskActionFlags); ok {
		return v
	}
	if key := ToolSessionKeyFromCtx(ctx); key != "" {
		if v, ok := sessionTaskActionFlags.Load(key); ok {
			return v.(*TaskActionFlags)
		}
	}
	return nil
}

// recordTaskAction is a helper that sets a flag if TaskActionFlags exist in context.
func recordTaskAction(ctx context.Context, setter func(*TaskActionFlags)) {
	if flags := TaskActionFlagsFromCtx(ctx); flags != nil {
		setter(flags)
	}
}
