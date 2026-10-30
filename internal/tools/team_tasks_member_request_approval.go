package tools

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) approveMemberRequest(ctx context.Context, team *store.TeamData, agentID uuid.UUID, task *store.TeamTaskData) *Result {
	if task.OwnerAgentID == nil {
		return ErrorResult("request has no assignee — assign it first via update")
	}
	if *task.OwnerAgentID == team.LeadAgentID {
		return ErrorResult("cannot dispatch a request assigned to the team lead — reassign it to a team member first via update")
	}
	if err := t.clearLeaderApprovalFlag(ctx, task); err != nil {
		return ErrorResult("failed to approve request: " + err.Error())
	}
	if err := t.manager.Store().AssignTask(ctx, task.ID, *task.OwnerAgentID, team.ID); err != nil {
		return ErrorResult("failed to dispatch request: " + err.Error())
	}

	assignee := t.manager.AgentKeyFromID(ctx, *task.OwnerAgentID)
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskDispatched, BuildTaskEventPayload(
		team.ID.String(), task.ID.String(),
		store.TeamTaskStatusInProgress,
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithOwnerAgentKey(assignee),
		WithContextInfo(ctx),
	))
	_ = t.manager.Store().AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  task.ID,
		AgentID: &agentID,
		Content: "Request approved by the team lead and dispatched.",
	})

	t.manager.DispatchTaskToAgent(ctx, task, team, *task.OwnerAgentID)

	return NewResult(fmt.Sprintf("Request #%d \"%s\" (id: %s) approved and dispatched to %s.", task.TaskNumber, task.Subject, task.ID, assignee))
}

func (t *TeamTasksTool) clearLeaderApprovalFlag(ctx context.Context, task *store.TeamTaskData) error {
	if !TaskAwaitingLeaderApproval(task) {
		return nil
	}
	metadata := maps.Clone(task.Metadata)
	delete(metadata, TaskMetaAwaitingLeaderApproval)
	if err := t.manager.Store().UpdateTask(ctx, task.ID, map[string]any{"metadata": metadata}); err != nil {
		return err
	}
	task.Metadata = metadata
	return nil
}
