package orcaserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const testCode = "orca://pair?secret-token-123"

func installFakeOrca(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orca")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func TestCheckReadyRuntimeIsHealthy(t *testing.T) {
	installFakeOrca(t, `echo '{"ok":true,"result":{"runtime":{"state":"ready"}}}'`)
	h := NewChecker().Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusHealthy || !h.Healthy || h.RuntimeState != "ready" {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckTopLevelRuntimeShapeIsAccepted(t *testing.T) {
	installFakeOrca(t, `echo '{"runtime":{"state":"ready"}}'`)
	if h := NewChecker().Check(context.Background(), testCode); h.Status != store.OrcaServerStatusHealthy {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckNotReadyRuntimeIsUnhealthy(t *testing.T) {
	installFakeOrca(t, `echo '{"result":{"runtime":{"state":"connecting"}}}'; exit 1`)
	h := NewChecker().Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusUnhealthy || h.Healthy || h.RuntimeState != "connecting" {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckGarbageOutputIsError(t *testing.T) {
	installFakeOrca(t, `echo 'not json'`)
	h := NewChecker().Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusError || h.Healthy || h.Error == "" {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckCLIErrorMessageIsSurfaced(t *testing.T) {
	installFakeOrca(t, `echo '{"ok":false,"error":{"code":"invalid_argument","message":"Invalid remote pairing code"}}'`)
	h := NewChecker().Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusError || !strings.Contains(h.Error, "Invalid remote pairing code") {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckTimeoutKillsHungCLI(t *testing.T) {
	installFakeOrca(t, `sleep 30`)
	c := &Checker{Timeout: 300 * time.Millisecond}
	start := time.Now()
	h := c.Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusError || !strings.Contains(h.Error, "timed out") {
		t.Fatalf("unexpected health: %+v", h)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout did not stop the process promptly: %s", time.Since(start))
	}
}

func TestCheckMissingCLIReportsCLIMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h := NewChecker().Check(context.Background(), testCode)
	if h.Status != store.OrcaServerStatusCLIMissing || h.Error == "" {
		t.Fatalf("unexpected health: %+v", h)
	}
}

func TestCheckRedactsPairingCodeFromStderrAndOutput(t *testing.T) {
	installFakeOrca(t, `echo "failed to reach `+testCode+`" >&2; exit 2`)
	h := NewChecker().Check(context.Background(), testCode)
	if strings.Contains(h.Error, testCode) || strings.Contains(h.Error, "secret-token-123") || !strings.Contains(h.Error, "[redacted]") {
		t.Fatalf("pairing code leaked: %q", h.Error)
	}

	installFakeOrca(t, `echo '{"ok":false,"error":{"message":"bad `+testCode+`"}}'`)
	h = NewChecker().Check(context.Background(), testCode)
	if strings.Contains(h.Error, "secret-token-123") {
		t.Fatalf("pairing code leaked from stdout error: %q", h.Error)
	}
}

func TestCheckPassesOnlyPairingEnvWithoutGatewaySecrets(t *testing.T) {
	t.Setenv("GOCLAW_SECRET_PROBE", "leak-me")
	installFakeOrca(t, `echo "{\"runtime\":{\"state\":\"ready\"},\"probe\":\"$GOCLAW_SECRET_PROBE\",\"code\":\"$ORCA_PAIRING_CODE\"}" > "$HOME/env.out"; echo '{"runtime":{"state":"ready"}}'`)
	home := t.TempDir()
	t.Setenv("HOME", home)
	NewChecker().Check(context.Background(), testCode)
	b, err := os.ReadFile(filepath.Join(home, "env.out"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "leak-me") || !strings.Contains(string(b), testCode) {
		t.Fatalf("unexpected child env: %s", b)
	}
}

func TestSanitizeStripsControlCharsAndTruncates(t *testing.T) {
	got := sanitize("a\x00b\nc"+strings.Repeat("x", 500), "")
	if strings.ContainsAny(got, "\x00\n") || len([]rune(got)) != maxErrorRunes {
		t.Fatalf("unexpected sanitize result len=%d %q", len([]rune(got)), got[:10])
	}
}
