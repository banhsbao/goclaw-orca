package providers

import "testing"

func TestACPProviderMCPServers(t *testing.T) {
	p := &ACPProvider{}
	WithACPMCPBridge("http://gw:18790/mcp/bridge", "gw-secret")(p)
	req := ChatRequest{Options: map[string]any{OptAgentID: "agent-1", OptUserID: "u1", OptChannel: "telegram"}}

	servers := p.mcpServers(req, "sess")
	if len(servers) != 1 || servers[0].Type != "http" || servers[0].URL != "http://gw:18790/mcp/bridge" {
		t.Fatalf("servers = %+v", servers)
	}
	var hasSig bool
	for _, h := range servers[0].Headers {
		if h.Name == "Authorization" {
			t.Fatal("gateway token must not be sent to the ACP agent")
		}
		hasSig = hasSig || (h.Name == "X-Bridge-Sig" && h.Value != "")
	}
	if !hasSig {
		t.Fatal("missing X-Bridge-Sig")
	}

	if mcpServersKey(servers) != mcpServersKey(p.mcpServers(req, "sess")) {
		t.Fatal("key must be stable for the same context")
	}
	if mcpServersKey(servers) == mcpServersKey(p.mcpServers(req, "other")) {
		t.Fatal("key must change with the session context")
	}
	if got := p.mcpServers(ChatRequest{}, "sess"); got != nil {
		t.Fatalf("no agent context should yield no servers, got %+v", got)
	}
	if got := (&ACPProvider{}).mcpServers(req, "sess"); got != nil {
		t.Fatalf("bridge disabled should yield no servers, got %+v", got)
	}
}
