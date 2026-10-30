package methods

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type approveTaskStore struct {
	*humanTaskStore
	approved int
}

func (s *approveTaskStore) UpdateTask(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if md, ok := updates["metadata"].(map[string]any); ok {
		s.task.Metadata = md
	}
	return nil
}

func (s *approveTaskStore) ApproveTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approved++
	s.task.Status = store.TeamTaskStatusCompleted
	return nil
}

func newApproveFixture(t *testing.T, status string, held bool) (*humanTaskFixture, *approveTaskStore) {
	t.Helper()
	f := newHumanTaskFixture(t, status)
	f.st.task.TaskType = "request"
	f.st.task.Metadata = map[string]any{"team_workspace": "/ws"}
	if held {
		f.st.task.Metadata[tools.TaskMetaAwaitingLeaderApproval] = true
	}
	st := &approveTaskStore{humanTaskStore: f.st}
	f.m = &TeamsMethods{teamStore: st}
	return f, st
}

func TestTaskApprove_HeldMemberRequestIsDispatchedToOwner(t *testing.T) {
	f, st := newApproveFixture(t, store.TeamTaskStatusPending, true)

	ok, code, msg := f.call(t, f.m.handleTaskApprove, map[string]any{"teamId": f.teamID.String(), "taskId": f.taskID.String()})
	if !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if st.approved != 0 {
		t.Fatal("a held request must not go through ApproveTask (that would mark it completed)")
	}
	if len(f.st.assigned) != 1 || f.st.assigned[0] != f.member {
		t.Fatalf("assigned = %v, want the request owner", f.st.assigned)
	}
	if f.st.task.Status != store.TeamTaskStatusInProgress {
		t.Fatalf("status = %s, want in_progress", f.st.task.Status)
	}
	if _, still := f.st.task.Metadata[tools.TaskMetaAwaitingLeaderApproval]; still {
		t.Fatal("hold flag must be cleared")
	}
	if f.st.task.Metadata["team_workspace"] != "/ws" {
		t.Fatal("other metadata must be kept")
	}
	if len(f.st.comments) != 1 {
		t.Fatalf("comments = %v, want one approval comment", f.st.comments)
	}
}

func TestTaskApprove_HeldRequestOwnedByLeadIsRejected(t *testing.T) {
	f, _ := newApproveFixture(t, store.TeamTaskStatusPending, true)
	lead := f.leadID
	f.st.task.OwnerAgentID = &lead

	ok, _, _ := f.call(t, f.m.handleTaskApprove, map[string]any{"teamId": f.teamID.String(), "taskId": f.taskID.String()})
	if ok {
		t.Fatal("expected rejection for a lead-owned request")
	}
	if len(f.st.assigned) != 0 {
		t.Fatal("request must not be assigned")
	}
}

func TestTaskApprove_InReviewTaskStillUsesApproveTask(t *testing.T) {
	f, st := newApproveFixture(t, store.TeamTaskStatusInReview, false)

	ok, code, msg := f.call(t, f.m.handleTaskApprove, map[string]any{"teamId": f.teamID.String(), "taskId": f.taskID.String()})
	if !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if st.approved != 1 || len(f.st.assigned) != 0 {
		t.Fatalf("approved=%d assigned=%v, want the existing in_review approval path", st.approved, f.st.assigned)
	}
}

func TestTaskApprove_WrongTeamIsNotFound(t *testing.T) {
	f, st := newApproveFixture(t, store.TeamTaskStatusPending, true)

	ok, _, _ := f.call(t, f.m.handleTaskApprove, map[string]any{"teamId": uuid.New().String(), "taskId": f.taskID.String()})
	if ok {
		t.Fatal("expected not found for a task of another team")
	}
	if st.approved != 0 || len(f.st.assigned) != 0 {
		t.Fatal("nothing must change for a task of another team")
	}
}
