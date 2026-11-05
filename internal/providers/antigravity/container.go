package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ModelInfo struct {
	ID   string
	Name string
}

func EnsureContainer(ctx context.Context, id uuid.UUID, dataDir string) error {
	if err := Ready(); err != nil {
		return err
	}
	name := ContainerName(id)
	out, err := docker(ctx, "inspect", "-f", "{{.State.Running}}", name)
	switch {
	case err == nil && strings.TrimSpace(out) == "true":
		return ConnectContainer(ctx, id)
	case err == nil:
		if _, err = docker(ctx, "start", name); err != nil {
			return err
		}
		return ConnectContainer(ctx, id)
	}
	ok, err := imageReady(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPreparing
	}
	dataVol, err := dataVolume(ctx, dataDir)
	if err != nil {
		return err
	}
	workDir := WorkDir(dataDir, id)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create agy workspace: %w", err)
	}
	subpath := filepath.ToSlash(filepath.Join("acp-workspaces", "agy-"+id.String()))
	_, err = docker(ctx, "run", "-d", "--name", name, "--restart", "unless-stopped",
		"--user", "1000:1000", "-e", "HOME="+homeMountDir,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", "512", "--memory", "2g",
		"--label", containerLbl+"="+id.String(),
		"-v", filepath.Dir(hostBinary())+":"+binMountDir+":ro",
		"-v", HomeVolume(id)+":"+homeMountDir,
		"--mount", fmt.Sprintf("type=volume,src=%s,dst=%s,volume-subpath=%s", dataVol, workDir, subpath),
		Image, "sleep", "infinity")
	if err != nil && !strings.Contains(err.Error(), "is already in use by container") {
		return err
	}
	return ConnectContainer(ctx, id)
}

func dataVolume(ctx context.Context, dataDir string) (string, error) {
	self, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("%w: hostname: %v", ErrNotConfigured, err)
	}
	out, err := docker(ctx, "inspect", "-f", "{{json .Mounts}}", self)
	if err != nil {
		return "", fmt.Errorf("%w: gateway must run in Docker: %v", ErrNotConfigured, err)
	}
	var mounts []struct {
		Type        string
		Name        string
		Destination string
	}
	if err := json.Unmarshal([]byte(out), &mounts); err != nil {
		return "", fmt.Errorf("%w: parse mounts: %v", ErrNotConfigured, err)
	}
	want := filepath.Clean(dataDir)
	for _, m := range mounts {
		if m.Type == "volume" && filepath.Clean(m.Destination) == want {
			return m.Name, nil
		}
	}
	return "", fmt.Errorf("%w: no data volume mounted at %s", ErrNotConfigured, want)
}

func Remove(ctx context.Context, id uuid.UUID) error {
	var errs []string
	if _, err := docker(ctx, "rm", "-f", ContainerName(id)); err != nil && !strings.Contains(err.Error(), "No such") {
		errs = append(errs, err.Error())
	}
	if _, err := docker(ctx, "volume", "rm", "-f", HomeVolume(id)); err != nil && !strings.Contains(err.Error(), "No such") {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func Models(ctx context.Context, id uuid.UUID) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := docker(ctx, "exec", ContainerName(id), agyPath(), "models")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotAuthenticated, err)
	}
	return parseModels(out), nil
}
