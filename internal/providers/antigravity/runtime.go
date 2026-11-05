package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	BinaryEnv    = "GOCLAW_AGY_BINARY_HOST_PATH"
	binMountDir  = "/opt/agy-bin"
	homeMountDir = "/home/agy"
	containerLbl = "goclaw.antigravity.provider"
	maxStderr    = 64 * 1024
)

var (
	ErrNotConfigured    = errors.New("antigravity runtime not configured")
	ErrNotAuthenticated = errors.New("antigravity not authenticated")
	ErrNoLoginSession   = errors.New("no login session")
	ErrInvalidCode      = errors.New("invalid authorization code")
	ErrLoginFailed      = errors.New("antigravity login failed")
	ErrPreparing        = errors.New("antigravity image is being prepared")
)

func ContainerName(id uuid.UUID) string { return "goclaw-agy-" + id.String() }

func HomeVolume(id uuid.UUID) string { return "goclaw-agy-home-" + id.String() }

func WorkDir(dataDir string, id uuid.UUID) string {
	return filepath.Join(filepath.Clean(dataDir), "acp-workspaces", "agy-"+id.String())
}

func hostBinary() string { return os.Getenv(BinaryEnv) }

func agyPath() string { return binMountDir + "/" + filepath.Base(hostBinary()) }

const QAOnlyRule = "You are a pure question-answering assistant. Never call, use or request any tool: no shell commands, no file access, no browser, no web search, no subagents. Answer directly in plain text from your own knowledge and the conversation."

const BridgeToolsRule = "Use only the tools provided by the \"goclaw\" MCP server. Never use your built-in tools: no shell commands, no local file access, no built-in browser, no built-in web search, no subagents. When a task needs delegation, task tracking, messaging, files or web access, call the matching goclaw MCP tool instead of describing what you would do."

func SessionMeta(bridgeTools bool) json.RawMessage {
	rule := QAOnlyRule
	if bridgeTools {
		rule = BridgeToolsRule
	}
	data, _ := json.Marshal(map[string]any{"antigravity": map[string]any{"systemPrompt": map[string]string{"append": rule}}})
	return data
}

func ACPArgs(id uuid.UUID, workDir string) []string {
	return []string{"exec", "-i", "-w", workDir, ContainerName(id), agyPath(), "acp", "--dangerously-skip-permissions"}
}

func Ready() error {
	if !filepath.IsAbs(hostBinary()) {
		return fmt.Errorf("%w: %s must be an absolute path", ErrNotConfigured, BinaryEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("%w: docker CLI not found", ErrNotConfigured)
	}
	return nil
}

func docker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	stderr := &cappedBuf{max: maxStderr}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

type cappedBuf struct {
	max int
	buf bytes.Buffer
}

func (c *cappedBuf) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *cappedBuf) String() string { return c.buf.String() }
