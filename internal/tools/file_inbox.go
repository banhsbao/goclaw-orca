package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const inboxDirName = "inbox"

type FileInboxTool struct {
	workspace string
	tickets   *InboxTicketStore
}

func NewFileInboxTool(workspace string, tickets *InboxTicketStore) *FileInboxTool {
	return &FileInboxTool{workspace: workspace, tickets: tickets}
}

func (t *FileInboxTool) Name() string { return "file_inbox" }

func (t *FileInboxTool) Description() string {
	return "Create a short-lived upload URL so a program on another machine (for example a coding agent started through the orca CLI) can push files into this agent's workspace inbox. " +
		"Put the returned curl command in the brief for that program. Uploaded files land in the inbox/ folder of the workspace; deliver them to the user with send_file."
}

func (t *FileInboxTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *FileInboxTool) Execute(ctx context.Context, _ map[string]any) *Result {
	workspace := ToolTeamWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = ToolWorkspaceFromCtx(ctx)
	}
	if workspace == "" {
		workspace = t.workspace
	}
	if workspace == "" {
		return ErrorResult("workspace is not available for this run")
	}
	base := strings.TrimRight(t.tickets.BaseURL(), "/")
	if base == "" {
		return ErrorResult("gateway public URL is not known yet; set gateway.public_url so remote machines can reach the upload endpoint")
	}

	dir := filepath.Join(workspace, inboxDirName)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return ErrorResult("failed to prepare inbox: " + err.Error())
	}
	id, expires, err := t.tickets.Issue(dir, InboxTicketTTL, InboxTicketMaxFiles)
	if err != nil {
		return ErrorResult("failed to issue upload ticket: " + err.Error())
	}

	url := fmt.Sprintf("%s/v1/inbox/%s/", base, id)
	return NewResult(fmt.Sprintf(
		"Upload URL ready (valid until %s UTC, up to %d files, %d MB each).\n"+
			"Run on the remote machine for each file: curl -fsS -T <local-file> %s\n"+
			"Files arrive in %s/<filename>. After the remote program finishes, verify with list_files and deliver with send_file.\n"+
			"Treat the URL as a secret: do not echo it to the user.",
		expires.UTC().Format("15:04:05"), InboxTicketMaxFiles, MaxFileSizeBytes/(1024*1024), url, dir))
}
