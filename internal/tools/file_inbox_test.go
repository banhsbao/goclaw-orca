package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileInboxTool_IssuesTicketForWorkspaceInbox(t *testing.T) {
	ws := t.TempDir()
	store := NewInboxTicketStore()
	store.SetBaseURL(func() string { return "https://gw.example.com/" })
	tool := NewFileInboxTool(ws, store)

	res := tool.Execute(context.Background(), nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "curl -fsS -T <local-file> https://gw.example.com/v1/inbox/") {
		t.Fatalf("missing curl command: %s", res.ForLLM)
	}
	if fi, err := os.Stat(filepath.Join(ws, "inbox")); err != nil || !fi.IsDir() {
		t.Fatalf("inbox dir not created: %v", err)
	}
}

func TestFileInboxTool_ErrorsWithoutPublicURL(t *testing.T) {
	tool := NewFileInboxTool(t.TempDir(), NewInboxTicketStore())
	if res := tool.Execute(context.Background(), nil); !res.IsError {
		t.Fatal("expected error when base URL is unknown")
	}
}

func TestInboxTicketStore_RefundRestoresUpload(t *testing.T) {
	s := NewInboxTicketStore()
	id, _, _ := s.Issue("/d", InboxTicketTTL, 1)
	if _, err := s.Consume(id); err != nil {
		t.Fatal(err)
	}
	s.Refund(id)
	if _, err := s.Consume(id); err != nil {
		t.Fatalf("refund did not restore upload: %v", err)
	}
}
