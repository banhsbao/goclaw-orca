package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

// Initialize sends the ACP initialize request to establish capabilities.
func (p *ACPProcess) Initialize(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req := InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo:      ClientInfo{Name: "GoClaw", Version: "1.0"},
		Capabilities:    ClientCaps{},
	}
	var resp InitializeResponse
	if err := p.conn.Call(ctx, "initialize", req, &resp); err != nil {
		return fmt.Errorf("acp initialize: %w", err)
	}
	p.agentCaps = resp.Capabilities
	slog.Info("acp: initialized", "agent", resp.AgentInfo.Name, "version", resp.AgentInfo.Version, "loadSession", resp.Capabilities.LoadSession, "mcpCaps", resp.Capabilities.MCPCapabilities)
	return nil
}

// NewSession creates a new ACP session and returns its session ID.
func (p *ACPProcess) NewSession(ctx context.Context) (string, error) {
	return p.NewSessionWithMeta(ctx, nil, nil)
}

func (p *ACPProcess) NewSessionWithMeta(ctx context.Context, meta json.RawMessage, servers []MCPServer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cwd := p.workDir
	if cwd == "" {
		cwd, _ = filepath.Abs(".")
	}

	req := NewSessionRequest{
		Cwd:        cwd,
		McpServers: nonNilServers(servers),
		Meta:       meta,
	}
	var resp NewSessionResponse
	if err := p.conn.Call(ctx, "session/new", req, &resp); err != nil {
		return "", fmt.Errorf("acp session/new: %w", err)
	}
	slog.Info("acp: session/new", "sid", resp.SessionID, "cwd", cwd, "mcp_servers", len(servers))
	return resp.SessionID, nil
}

// LoadSession restores a previous ACP session by ID (used after process restart).
// Returns the session ID to use going forward (may equal the requested ID).
// Only call if AgentCaps().LoadSession is true.
func (p *ACPProcess) LoadSession(ctx context.Context, sessionID string) (string, error) {
	return p.LoadSessionWithMeta(ctx, sessionID, nil, nil)
}

func (p *ACPProcess) LoadSessionWithMeta(ctx context.Context, sessionID string, meta json.RawMessage, servers []MCPServer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cwd := p.workDir
	if cwd == "" {
		cwd, _ = filepath.Abs(".")
	}

	req := LoadSessionRequest{SessionID: sessionID, Cwd: cwd, McpServers: nonNilServers(servers), Meta: meta}
	var resp LoadSessionResponse
	if err := p.conn.Call(ctx, "session/load", req, &resp); err != nil {
		return "", fmt.Errorf("acp session/load: %w", err)
	}
	slog.Info("acp: session/load", "sid", resp.SessionID)
	return resp.SessionID, nil
}

// Prompt sends user content to sessionID and blocks until the agent completes,
// invoking onUpdate for each session/update notification received.
func (p *ACPProcess) Prompt(ctx context.Context, sessionID string, content []ContentBlock, onUpdate func(SessionUpdate)) (*PromptResponse, error) {
	p.inUse.Add(1)
	defer p.inUse.Add(-1)

	p.mu.Lock()
	p.lastActive = time.Now()
	p.mu.Unlock()

	p.registerUpdateFn(sessionID, onUpdate)
	defer p.unregisterUpdateFn(sessionID)

	goclawSession := goclawSessionFromCtx(ctx)
	slog.Info("acp: session/prompt", "session", goclawSession, "sid", sessionID)
	req := PromptRequest{
		SessionID: sessionID,
		Prompt:    content,
	}

	var resp PromptResponse
	if err := p.conn.Call(ctx, "session/prompt", req, &resp); err != nil {
		return nil, fmt.Errorf("acp session/prompt: %w", err)
	}

	p.mu.Lock()
	p.lastActive = time.Now()
	p.mu.Unlock()

	slog.Info("acp: session/prompt completed", "session", goclawSession, "sid", sessionID, "stopReason", resp.StopReason)
	return &resp, nil
}

// Cancel sends a session/cancel notification for the given session.
func (p *ACPProcess) Cancel(sessionID string) error {
	return p.conn.Notify("session/cancel", CancelNotification{
		SessionID: sessionID,
	})
}

func (p *ACPProcess) SetModel(ctx context.Context, sessionID, modelID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req := SetModelRequest{SessionID: sessionID, ModelID: modelID}
	if err := p.conn.Call(ctx, "session/set_model", req, nil); err != nil {
		return fmt.Errorf("acp session/set_model: %w", err)
	}
	return nil
}

func nonNilServers(servers []MCPServer) []MCPServer {
	if servers == nil {
		return []MCPServer{}
	}
	return servers
}
