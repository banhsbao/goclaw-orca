package providers

import (
	"sort"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

const acpBridgeServerName = "goclaw"

func WithACPMCPBridge(url, gatewayToken string) ACPOption {
	return func(p *ACPProvider) {
		p.bridgeURL = url
		p.bridgeToken = gatewayToken
	}
}

func (p *ACPProvider) mcpServers(req ChatRequest, sessionKey string) []acp.MCPServer {
	if p.bridgeURL == "" || p.bridgeToken == "" {
		return nil
	}
	bc := bridgeContextFromOpts(req.Options)
	if bc.AgentID == "" && bc.UserID == "" {
		return nil
	}
	headers := BridgeHeaders(p.bridgeToken, sessionKey, bc, false)
	if headers["X-Bridge-Sig"] == "" {
		return nil
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]acp.HTTPHeader, 0, len(names))
	for _, name := range names {
		list = append(list, acp.HTTPHeader{Name: name, Value: headers[name]})
	}
	return []acp.MCPServer{{Type: "http", Name: acpBridgeServerName, URL: p.bridgeURL, Headers: list}}
}

func mcpServersKey(servers []acp.MCPServer) string {
	var b strings.Builder
	for _, s := range servers {
		b.WriteString(s.Name + "|" + s.URL)
		for _, h := range s.Headers {
			if h.Name == "X-Bridge-Sig" {
				b.WriteString("|" + h.Value)
			}
		}
		b.WriteString(";")
	}
	return b.String()
}
