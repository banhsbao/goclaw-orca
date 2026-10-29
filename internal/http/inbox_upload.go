package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type InboxUploadHandler struct {
	tickets *tools.InboxTicketStore
}

func NewInboxUploadHandler(tickets *tools.InboxTicketStore) *InboxUploadHandler {
	return &InboxUploadHandler{tickets: tickets}
}

func (h *InboxUploadHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/inbox/{ticket}/{filename}", h.handlePut)
}

func (h *InboxUploadHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	ticket := r.PathValue("ticket")
	name := filepath.Base(r.PathValue("filename"))
	if name == "." || name == ".." || name == "/" || strings.ContainsAny(name, "\x00\\") || strings.HasPrefix(name, ".") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}
	if tools.IsBlockedExtension(strings.ToLower(filepath.Ext(name))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file type not allowed"})
		return
	}

	dir, err := h.tickets.Consume(ticket)
	if err != nil {
		slog.Warn("security.inbox_ticket_rejected", "remote", r.RemoteAddr, "error", err)
		status := http.StatusUnauthorized
		if errors.Is(err, tools.ErrInboxTicketUsedUp) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	size, err := writeInboxFile(w, r, dir, name)
	if err != nil {
		h.tickets.Refund(ticket)
		status := http.StatusInternalServerError
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": "upload failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"filename": name,
		"path":     filepath.Join(dir, name),
		"size":     size,
	})
}

func writeInboxFile(w http.ResponseWriter, r *http.Request, dir, name string) (int64, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	body := http.MaxBytesReader(w, r.Body, tools.MaxFileSizeBytes)
	size, err := io.Copy(tmp, body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(tmpPath, 0640); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		return 0, err
	}
	return size, nil
}
