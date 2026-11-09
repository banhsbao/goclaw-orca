package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type finishRecorder struct {
	calls   int
	content string
}

func (r *finishRecorder) fn(_ context.Context, outcome scheduler.RunOutcome, _ *tools.TaskActionFlags) {
	r.calls++
	if outcome.Result != nil {
		r.content = outcome.Result.Content
	}
}

func okOutcome(content string) scheduler.RunOutcome {
	return scheduler.RunOutcome{Result: &agent.RunResult{Content: content}}
}

func TestPendingTeamTask_ClosesAfterDelegationReported(t *testing.T) {
	rec := &finishRecorder{}
	lockStopped := false
	p := deferTeamTaskUntilDelegations(context.Background(), []string{"d-close-1"}, "task-1", "team-1",
		func() { lockStopped = true }, okOutcome("handed off"), &tools.TaskActionFlags{}, rec.fn)
	defer p.timer.Stop()

	if rec.calls != 0 {
		t.Fatal("task must stay open while the delegation runs")
	}
	got := claimPendingTeamTask([]string{"d-close-1"})
	if got != p {
		t.Fatalf("expected pending task for delegation, got %v", got)
	}
	got.resolveAfterAnnounce(context.Background(), okOutcome("engineer done, verified"), &tools.TaskActionFlags{})
	if rec.calls != 1 || rec.content != "engineer done, verified" || !lockStopped {
		t.Fatalf("calls=%d content=%q lockStopped=%v", rec.calls, rec.content, lockStopped)
	}
	if claimPendingTeamTask([]string{"d-close-1"}) != nil {
		t.Fatal("delegation must be released after close")
	}
}

func TestPendingTeamTask_RedelegationKeepsTaskOpen(t *testing.T) {
	rec := &finishRecorder{}
	p := deferTeamTaskUntilDelegations(context.Background(), []string{"d-slice-1"}, "task-2", "team-1",
		nil, okOutcome("slice 1 started"), &tools.TaskActionFlags{}, rec.fn)
	defer p.timer.Stop()

	next := &tools.TaskActionFlags{}
	next.AddAsyncDelegation("d-slice-2")
	claimPendingTeamTask([]string{"d-slice-1"}).resolveAfterAnnounce(context.Background(), okOutcome("slice 2 started"), next)
	if rec.calls != 0 {
		t.Fatal("a new async delegation must keep the task open")
	}
	claimPendingTeamTask([]string{"d-slice-2"}).resolveAfterAnnounce(context.Background(), okOutcome("all slices done"), &tools.TaskActionFlags{})
	if rec.calls != 1 || rec.content != "all slices done" {
		t.Fatalf("calls=%d content=%q", rec.calls, rec.content)
	}
}

func TestPendingTeamTask_FailedAnnounceRunClosesTask(t *testing.T) {
	rec := &finishRecorder{}
	p := deferTeamTaskUntilDelegations(context.Background(), []string{"d-fail-1", "d-fail-2"}, "task-3", "team-1",
		nil, okOutcome("handed off"), &tools.TaskActionFlags{}, rec.fn)
	defer p.timer.Stop()

	claimPendingTeamTask([]string{"d-fail-1"}).resolveAfterAnnounce(context.Background(),
		scheduler.RunOutcome{Err: errors.New("boom")}, &tools.TaskActionFlags{})
	if rec.calls != 1 {
		t.Fatal("a failed member run must close the task even with delegations outstanding")
	}
	if claimPendingTeamTask([]string{"d-fail-2"}) != nil {
		t.Fatal("outstanding delegations must be released when the task closes")
	}
}

func TestPendingTeamTask_WaitsForAllParallelDelegations(t *testing.T) {
	rec := &finishRecorder{}
	p := deferTeamTaskUntilDelegations(context.Background(), []string{"d-par-1", "d-par-2"}, "task-4", "team-1",
		nil, okOutcome("handed off"), &tools.TaskActionFlags{}, rec.fn)
	defer p.timer.Stop()

	claimPendingTeamTask([]string{"d-par-1"}).resolveAfterAnnounce(context.Background(), okOutcome("half"), &tools.TaskActionFlags{})
	if rec.calls != 0 {
		t.Fatal("must wait for the second delegation")
	}
	claimPendingTeamTask([]string{"d-par-2"}).resolveAfterAnnounce(context.Background(), okOutcome("both"), &tools.TaskActionFlags{})
	if rec.calls != 1 || rec.content != "both" {
		t.Fatalf("calls=%d content=%q", rec.calls, rec.content)
	}
}

func TestTaskActionFlags_ResolvedBySessionKeyForBridgeCalls(t *testing.T) {
	flags := &tools.TaskActionFlags{}
	unregister := tools.RegisterSessionTaskActionFlags("agent:tech-lead:team:x:1", flags)
	ctx := tools.WithToolSessionKey(context.Background(), "agent:tech-lead:team:x:1")
	if tools.TaskActionFlagsFromCtx(ctx) != flags {
		t.Fatal("bridge context must resolve the run's flags by session key")
	}
	unregister()
	if tools.TaskActionFlagsFromCtx(ctx) != nil {
		t.Fatal("flags must be gone after the run ends")
	}
}
