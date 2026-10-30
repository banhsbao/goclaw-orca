package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const memberRequestOutcomeMaxRunes = 4000

// commentMemberRequestOutcomeOnParent posts a finished member request's result
// on the task that raised it, so the requester sees the answer when it is
// retried and the lead sees both sides on one task.
func commentMemberRequestOutcomeOnParent(ctx context.Context, teamStore store.TeamStore, taskID uuid.UUID, ownerKey string) {
	task, err := teamStore.GetTask(ctx, taskID)
	if err != nil || task == nil || task.TaskType != "request" || task.ParentID == nil {
		return
	}
	switch task.Status {
	case store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled:
	default:
		return
	}

	result := ""
	if task.Result != nil {
		result = *task.Result
	}
	if runes := []rune(result); len(runes) > memberRequestOutcomeMaxRunes {
		result = string(runes[:memberRequestOutcomeMaxRunes]) + fmt.Sprintf("\n[truncated, full result on request #%d]", task.TaskNumber)
	}
	if result == "" {
		result = "(no result text)"
	}

	content := fmt.Sprintf("Request #%d \"%s\" %s by %s.\n\n%s", task.TaskNumber, task.Subject, task.Status, ownerKey, result)
	if err := teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  *task.ParentID,
		AgentID: task.OwnerAgentID,
		Content: content,
	}); err != nil {
		slog.Warn("member request: failed to comment outcome on parent task",
			"task_id", task.ID, "parent_id", *task.ParentID, "error", err)
	}
}
