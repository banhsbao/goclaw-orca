package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const embeddedMediaMinChars = 2048

var embeddedMediaTypes = []struct {
	magic []byte
	ext   string
	mime  string
}{
	{[]byte("\x89PNG\r\n\x1a\n"), "png", "image/png"},
	{[]byte("\xff\xd8\xff"), "jpg", "image/jpeg"},
	{[]byte("GIF8"), "gif", "image/gif"},
	{[]byte("%PDF-"), "pdf", "application/pdf"},
}

func (t *ExecTool) extractCredentialedMedia(ctx context.Context, stdout string) string {
	if len(stdout) < embeddedMediaMinChars {
		return stdout
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return stdout
	}
	var doc any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return stdout
	}
	dir := t.embeddedMediaDir(ctx)
	changed := false
	doc = replaceEmbeddedMedia(doc, dir, &changed)
	if !changed {
		return stdout
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return stdout
	}
	return string(out)
}

func (t *ExecTool) embeddedMediaDir(ctx context.Context) string {
	workspace := ToolTeamWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = ToolWorkspaceFromCtx(ctx)
	}
	if workspace == "" {
		workspace = t.workspace
	}
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, inboxDirName)
}

func replaceEmbeddedMedia(v any, dir string, changed *bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = replaceEmbeddedMedia(val, dir, changed)
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = replaceEmbeddedMedia(val, dir, changed)
		}
		return x
	case string:
		if len(x) < embeddedMediaMinChars {
			return x
		}
		raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(x))
		if err != nil {
			return x
		}
		for _, mt := range embeddedMediaTypes {
			if bytes.HasPrefix(raw, mt.magic) {
				*changed = true
				return saveEmbeddedMedia(dir, raw, mt.ext, mt.mime)
			}
		}
		return x
	}
	return v
}

func saveEmbeddedMedia(dir string, raw []byte, ext, mime string) string {
	if dir == "" {
		return fmt.Sprintf("[%s, %d bytes omitted: no workspace to save into]", mime, len(raw))
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Warn("credentialed_exec.media_extract_failed", "error", err)
		return fmt.Sprintf("[%s, %d bytes omitted: could not prepare inbox]", mime, len(raw))
	}
	name := fmt.Sprintf("capture-%d.%s", time.Now().UnixNano(), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0640); err != nil {
		slog.Warn("credentialed_exec.media_extract_failed", "error", err)
		return fmt.Sprintf("[%s, %d bytes omitted: could not save file]", mime, len(raw))
	}
	return fmt.Sprintf("[%s, %d bytes saved to %s; deliver it to the user with send_file]", mime, len(raw), path)
}
