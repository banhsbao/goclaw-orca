package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func createMemberRequest(t *testing.T, autoDispatch bool) (*mockBackend, *TeamTasksTool, context.Context, *PendingTeamDispatch, *store.TeamTaskData, *Result) {
	t.Helper()
	mb, tool, _, memberID, ctx := newTestTeamSetup()
	settings := `{"member_requests":{"enabled":true,"auto_dispatch":false}}`
	if autoDispatch {
		settings = `{"member_requests":{"enabled":true,"auto_dispatch":true}}`
	}
	mb.team.Settings = json.RawMessage(settings)
	mb.taskStore.team = mb.team
	parentID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

	ptd := NewPendingTeamDispatch()
	ptd.MarkListed()
	memberCtx := WithPendingTeamDispatch(ctx, ptd)
	memberCtx = store.WithAgentID(memberCtx, memberID)
	memberCtx = WithToolChannel(memberCtx, "telegram")
	memberCtx = WithTeamTaskID(memberCtx, parentID.String())
	memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

	result := tool.Execute(memberCtx, map[string]any{
		"action":      "create",
		"subject":     "Need the production tarball",
		"description": "Upload cafe-landing-production.tgz to the team inbox",
		"task_type":   "request",
		"assignee":    "member2-agent",
	})
	if result.IsError {
		t.Fatalf("create request: %s", result.ForLLM)
	}

	mb.taskStore.mu.Lock()
	defer mb.taskStore.mu.Unlock()
	for _, task := range mb.taskStore.tasks {
		if task.TaskType == "request" {
			return mb, tool, ctx, ptd, task, result
		}
	}
	t.Fatal("request task not stored")
	return nil, nil, nil, nil, nil, nil
}

func TestMemberRequest_WithoutAutoDispatchWaitsForLeader(t *testing.T) {
	_, _, _, ptd, task, result := createMemberRequest(t, false)

	if !TaskAwaitingLeaderApproval(task) {
		t.Fatal("request must carry the awaiting_leader_approval flag")
	}
	if task.Status != store.TeamTaskStatusPending {
		t.Fatalf("status = %s, want pending", task.Status)
	}
	if task.ParentID == nil {
		t.Fatal("request must link to the member's current task")
	}
	if len(ptd.Drain()) != 0 {
		t.Fatal("request must not be queued for post-turn dispatch")
	}
	if !strings.Contains(result.ForLLM, "waits for the team lead to approve") {
		t.Fatalf("member was not told the request waits for approval: %s", result.ForLLM)
	}
}

func TestMemberRequest_AutoDispatchSkipsApproval(t *testing.T) {
	_, _, _, ptd, task, _ := createMemberRequest(t, true)

	if TaskAwaitingLeaderApproval(task) {
		t.Fatal("auto-dispatched request must not wait for approval")
	}
	if len(ptd.Drain()) != 1 {
		t.Fatal("auto-dispatched request must be queued for post-turn dispatch")
	}
}

func TestMemberRequest_LeaderApproveDispatches(t *testing.T) {
	mb, tool, ctx, _, task, _ := createMemberRequest(t, false)

	leadCtx := store.WithAgentID(ctx, testLeadID)
	leadCtx = WithToolChannel(leadCtx, "telegram")
	result := tool.Execute(leadCtx, map[string]any{"action": "approve", "task_id": task.ID.String()})
	if result.IsError {
		t.Fatalf("approve: %s", result.ForLLM)
	}

	mb.taskStore.mu.Lock()
	stored := mb.taskStore.tasks[task.ID]
	mb.taskStore.mu.Unlock()
	if TaskAwaitingLeaderApproval(stored) {
		t.Fatal("approval flag must be cleared")
	}
	if stored.Status != store.TeamTaskStatusInProgress {
		t.Fatalf("status = %s, want in_progress", stored.Status)
	}
	if len(mb.dispatches) != 1 || mb.dispatches[0].TaskID != task.ID || mb.dispatches[0].AgentID != testMember2ID {
		t.Fatalf("dispatches = %+v, want one dispatch of the request to member2", mb.dispatches)
	}
}

func TestMemberRequest_MemberCannotApprove(t *testing.T) {
	mb, tool, ctx, _, task, _ := createMemberRequest(t, false)

	memberCtx := store.WithAgentID(ctx, testMemberID)
	memberCtx = WithToolChannel(memberCtx, "telegram")
	result := tool.Execute(memberCtx, map[string]any{"action": "approve", "task_id": task.ID.String()})
	if !result.IsError {
		t.Fatal("member must not approve its own request")
	}
	if len(mb.dispatches) != 0 {
		t.Fatal("request must not be dispatched")
	}
}

type recoverableTaskStore struct {
	*mockTaskStore
}

func (s recoverableTaskStore) ListRecoverableTasks(_ context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.TeamTaskData
	for _, task := range s.tasks {
		if task.TeamID == teamID && task.Status == store.TeamTaskStatusPending {
			out = append(out, *task)
		}
	}
	return out, nil
}

func TestDispatchUnblockedTasks_SkipsRequestsAwaitingApproval(t *testing.T) {
	mb, _, ctx, _, task, _ := createMemberRequest(t, false)

	manager := NewTeamToolManager(recoverableTaskStore{mb.taskStore}, nil, nil, t.TempDir())
	manager.DispatchUnblockedTasks(ctx, testTeamID)

	mb.taskStore.mu.Lock()
	stored := mb.taskStore.tasks[task.ID]
	mb.taskStore.mu.Unlock()
	if stored.Status != store.TeamTaskStatusPending {
		t.Fatalf("request awaiting approval was dispatched, status = %s", stored.Status)
	}
}
