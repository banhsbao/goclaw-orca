package cmd

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const pendingTeamTaskMaxWait = tools.DefaultDelegateRunTimeout + 15*time.Minute

type teamTaskFinisher func(ctx context.Context, outcome scheduler.RunOutcome, flags *tools.TaskActionFlags)

// pendingTeamTask holds a member's team task open while the work it handed off
// with async delegate is still running, so the task is closed (and the lead is
// told) only after the member has seen the delegated result.
type pendingTeamTask struct {
	TeamTaskID string
	TeamID     string

	mu        sync.Mutex
	remaining map[string]struct{}
	finish    teamTaskFinisher
	stopLock  func()
	timer     *time.Timer
	done      bool
}

var pendingTeamTasksByDelegation sync.Map

func deferTeamTaskUntilDelegations(
	ctx context.Context,
	delegationIDs []string,
	teamTaskID, teamID string,
	stopLock func(),
	fallbackOutcome scheduler.RunOutcome,
	fallbackFlags *tools.TaskActionFlags,
	finish teamTaskFinisher,
) *pendingTeamTask {
	p := &pendingTeamTask{
		TeamTaskID: teamTaskID,
		TeamID:     teamID,
		remaining:  map[string]struct{}{},
		finish:     finish,
		stopLock:   stopLock,
	}
	p.track(delegationIDs)
	p.timer = time.AfterFunc(pendingTeamTaskMaxWait, func() {
		slog.Warn("team task: delegated work never reported back, closing with member turn result",
			"task_id", teamTaskID)
		p.complete(ctx, fallbackOutcome, fallbackFlags)
	})
	slog.Info("team task: waiting for async delegations before closing",
		"task_id", teamTaskID, "delegations", len(delegationIDs))
	return p
}

func (p *pendingTeamTask) track(delegationIDs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range delegationIDs {
		p.remaining[id] = struct{}{}
		pendingTeamTasksByDelegation.Store(id, p)
	}
}

// claimPendingTeamTask returns the pending task that the given delegation
// results belong to, and marks those delegations as reported.
func claimPendingTeamTask(delegationIDs []string) *pendingTeamTask {
	var found *pendingTeamTask
	for _, id := range delegationIDs {
		v, ok := pendingTeamTasksByDelegation.LoadAndDelete(id)
		if !ok {
			continue
		}
		p := v.(*pendingTeamTask)
		p.mu.Lock()
		delete(p.remaining, id)
		p.mu.Unlock()
		if found == nil {
			found = p
		}
	}
	return found
}

// resolveAfterAnnounce runs after the member processed a delegation result.
// New async delegations from that turn keep the task open; otherwise the task
// is closed once no delegation is outstanding.
func (p *pendingTeamTask) resolveAfterAnnounce(ctx context.Context, outcome scheduler.RunOutcome, flags *tools.TaskActionFlags) {
	if next := flags.AsyncDelegations(); len(next) > 0 && outcome.Failure() == nil {
		p.track(next)
		return
	}
	p.mu.Lock()
	outstanding := len(p.remaining)
	p.mu.Unlock()
	if outstanding > 0 && outcome.Failure() == nil && !flags.Completed && !flags.Escalated {
		return
	}
	p.complete(ctx, outcome, flags)
}

func (p *pendingTeamTask) complete(ctx context.Context, outcome scheduler.RunOutcome, flags *tools.TaskActionFlags) {
	p.mu.Lock()
	if p.done {
		p.mu.Unlock()
		return
	}
	p.done = true
	for id := range p.remaining {
		pendingTeamTasksByDelegation.CompareAndDelete(id, p)
	}
	p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	if p.stopLock != nil {
		p.stopLock()
	}
	defer safego.Recover(nil, "component", "team_task_pending_delegations", "task_id", p.TeamTaskID)
	p.finish(ctx, outcome, flags)
}
