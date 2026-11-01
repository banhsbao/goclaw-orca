package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngPayload(size int) []byte {
	raw := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, size)...)
	return raw
}

func TestExtractCredentialedMedia_SavesImageAndStripsBase64(t *testing.T) {
	ws := t.TempDir()
	tool := NewExecTool(ws, false)
	raw := pngPayload(4000)
	stdout := `{"ok":true,"result":{"data":"` + base64.StdEncoding.EncodeToString(raw) + `","format":"png"}}`

	out := tool.extractCredentialedMedia(context.Background(), stdout)

	if strings.Contains(out, "iVBOR") {
		t.Fatal("base64 still present in output")
	}
	if !strings.Contains(out, "send_file") || !strings.Contains(out, `"format": "png"`) {
		t.Fatalf("unexpected output: %s", out)
	}
	files, _ := filepath.Glob(filepath.Join(ws, "inbox", "capture-*.png"))
	if len(files) != 1 {
		t.Fatalf("want 1 saved file, got %d", len(files))
	}
	got, _ := os.ReadFile(files[0])
	if string(got) != string(raw) {
		t.Fatal("saved bytes differ from original")
	}
}

func TestExtractCredentialedMedia_LeavesPlainOutputAlone(t *testing.T) {
	tool := NewExecTool(t.TempDir(), false)
	cases := []string{
		`{"ok":true}`,
		"plain text " + strings.Repeat("x", 3000),
		`{"data":"` + strings.Repeat("A", 3000) + `"}`,
		`{"data":"not base64 !!` + strings.Repeat("?", 3000) + `"}`,
	}
	for _, c := range cases {
		if got := tool.extractCredentialedMedia(context.Background(), c); got != c {
			t.Fatalf("output changed for %.40q", c)
		}
	}
}

func TestExtractCredentialedMedia_NoWorkspaceStillOmitsBlob(t *testing.T) {
	tool := NewExecTool("", false)
	stdout := `{"data":"` + base64.StdEncoding.EncodeToString(pngPayload(4000)) + `"}`
	out := tool.extractCredentialedMedia(context.Background(), stdout)
	if strings.Contains(out, "iVBOR") || !strings.Contains(out, "omitted") {
		t.Fatalf("unexpected output: %s", out)
	}
}
