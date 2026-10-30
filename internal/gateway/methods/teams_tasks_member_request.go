package methods

import (
	"context"
	"log/slog"
	"maps"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// approveHeldMemberRequest releases a member request that waits for leader
// approval: it clears the hold flag, assigns the request to its owner and
// dispatches it, mirroring the team lead's team_tasks approve.
func (m *TeamsMethods) approveHeldMemberRequest(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, locale string, task *store.TeamTaskData, teamID uuid.UUID, comment string) {
	if task.OwnerAgentID == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "request has no assignee — assign it first"))
		return
	}
	agentID := *task.OwnerAgentID
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		slog.Warn("teams.tasks.approve get team failed", "team_id", teamID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	if agentID == team.LeadAgentID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			"cannot dispatch a request assigned to the team lead — reassign it to a team member first"))
		return
	}

	metadata := maps.Clone(task.Metadata)
	delete(metadata, tools.TaskMetaAwaitingLeaderApproval)
	if err := m.teamStore.UpdateTask(ctx, task.ID, map[string]any{"metadata": metadata}); err != nil {
		slog.Warn("teams.tasks.approve clear hold failed", "task_id", task.ID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	task.Metadata = metadata
	if err := m.teamStore.AssignTask(ctx, task.ID, agentID, teamID); err != nil {
		slog.Warn("teams.tasks.approve assign failed", "task_id", task.ID, "agent_id", agentID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	if comment == "" {
		comment = "Request approved and dispatched."
	}
	m.addHumanComment(ctx, client, task.ID, comment)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true, "status": store.TeamTaskStatusInProgress}))

	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskDispatched, protocol.TeamTaskEventPayload{
		TeamID:     teamID.String(),
		TaskID:     task.ID.String(),
		TaskNumber: task.TaskNumber,
		Subject:    task.Subject,
		Status:     store.TeamTaskStatusInProgress,
		UserID:     client.UserID(),
		Channel:    taskOriginChannel(task),
		ChatID:     task.ChatID,
		Timestamp:  taskNowUTC(),
		ActorType:  "human",
		ActorID:    client.UserID(),
	}))
	m.dispatchTaskToAgent(ctx, task, task.ID, teamID, agentID, client.UserID())
}
