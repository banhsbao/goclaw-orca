package antigravity

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const Image = "goclaw-agy:bookworm-slim"

const dockerfile = "FROM debian:bookworm-slim\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git && rm -rf /var/lib/apt/lists/* && mkdir -p /home/agy && chown 1000:1000 /home/agy\n"

var imageBuild struct {
	mu       sync.Mutex
	building bool
	lastErr  error
}

func imageReady(ctx context.Context) (bool, error) {
	if _, err := docker(ctx, "image", "inspect", Image); err == nil {
		return true, nil
	}
	imageBuild.mu.Lock()
	defer imageBuild.mu.Unlock()
	if imageBuild.building {
		return false, nil
	}
	if err := imageBuild.lastErr; err != nil {
		imageBuild.lastErr = nil
		return false, err
	}
	imageBuild.building = true
	go runImageBuild()
	return false, nil
}

func runImageBuild() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	err := buildImage(ctx)
	imageBuild.mu.Lock()
	imageBuild.building = false
	imageBuild.lastErr = err
	imageBuild.mu.Unlock()
}

func buildImage(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", Image, "-")
	cmd.Stdin = strings.NewReader(dockerfile)
	stderr := &cappedBuf{max: maxStderr}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return &buildError{err: err, out: strings.TrimSpace(stderr.String())}
	}
	return nil
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return "docker build: " + e.err.Error() + ": " + e.out }

func (e *buildError) Unwrap() error { return e.err }
