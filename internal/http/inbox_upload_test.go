package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func newInboxTestServer(t *testing.T) (*tools.InboxTicketStore, *http.ServeMux, string) {
	t.Helper()
	store := tools.NewInboxTicketStore()
	mux := http.NewServeMux()
	NewInboxUploadHandler(store).RegisterRoutes(mux)
	return store, mux, t.TempDir()
}

func putInbox(mux *http.ServeMux, ticket, name, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/"+ticket+"/"+name, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestInboxUpload_StoresFileInTicketDir(t *testing.T) {
	store, mux, dir := newInboxTestServer(t)
	id, _, err := store.Issue(dir, time.Minute, 2)
	if err != nil {
		t.Fatal(err)
	}
	rec := putInbox(mux, id, "report.txt", "hello")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file content %q err %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %d entries", len(entries))
	}
}

func TestInboxUpload_RejectsUnknownTicket(t *testing.T) {
	_, mux, _ := newInboxTestServer(t)
	if rec := putInbox(mux, "nope", "a.txt", "x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestInboxUpload_EnforcesUploadLimit(t *testing.T) {
	store, mux, dir := newInboxTestServer(t)
	id, _, _ := store.Issue(dir, time.Minute, 1)
	if rec := putInbox(mux, id, "a.txt", "x"); rec.Code != http.StatusOK {
		t.Fatalf("first upload: %d", rec.Code)
	}
	if rec := putInbox(mux, id, "b.txt", "x"); rec.Code != http.StatusForbidden {
		t.Fatalf("second upload want 403, got %d", rec.Code)
	}
}

func TestInboxUpload_RejectsExpiredTicket(t *testing.T) {
	store, mux, dir := newInboxTestServer(t)
	id, _, _ := store.Issue(dir, -time.Second, 1)
	if rec := putInbox(mux, id, "a.txt", "x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestInboxUpload_RejectsUnsafeNames(t *testing.T) {
	store, mux, dir := newInboxTestServer(t)
	id, _, _ := store.Issue(dir, time.Minute, 10)
	for _, name := range []string{".hidden", "run.sh", "..%2Fescape.txt"} {
		rec := putInbox(mux, id, name, "x")
		if rec.Code == http.StatusOK {
			t.Fatalf("%q accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Fatal("file escaped ticket dir")
	}
}
