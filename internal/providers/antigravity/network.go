package antigravity

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const NetworkName = "goclaw-agy-net"

func ensureNetwork(ctx context.Context) error {
	if _, err := docker(ctx, "network", "inspect", NetworkName); err == nil {
		return nil
	}
	_, err := docker(ctx, "network", "create", "--driver", "bridge", NetworkName)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

func connectNetwork(ctx context.Context, container string) error {
	_, err := docker(ctx, "network", "connect", NetworkName, container)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

func ConnectContainer(ctx context.Context, id uuid.UUID) error {
	if err := ensureNetwork(ctx); err != nil {
		return err
	}
	return connectNetwork(ctx, ContainerName(id))
}

func GatewayBridgeURL(ctx context.Context, port int) (string, error) {
	self, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("%w: hostname: %v", ErrNotConfigured, err)
	}
	out, err := docker(ctx, "inspect", "-f", "{{.Name}}", self)
	if err != nil {
		return "", fmt.Errorf("%w: gateway must run in Docker: %v", ErrNotConfigured, err)
	}
	name := strings.TrimPrefix(strings.TrimSpace(out), "/")
	if name == "" {
		return "", fmt.Errorf("%w: empty gateway container name", ErrNotConfigured)
	}
	if err := ensureNetwork(ctx); err != nil {
		return "", err
	}
	if err := connectNetwork(ctx, name); err != nil {
		return "", err
	}
	return "http://" + net.JoinHostPort(name, strconv.Itoa(port)) + "/mcp/bridge", nil
}
