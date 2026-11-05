package providers

import "strings"

func BridgeHeaders(gatewayToken, sessionKey string, bc BridgeContext, withBearer bool) map[string]string {
	headers := make(map[string]string)
	if withBearer && gatewayToken != "" {
		headers["Authorization"] = "Bearer " + gatewayToken
	}
	set := func(name, value string) {
		if value != "" && !strings.ContainsAny(value, "\r\n\x00") {
			headers[name] = value
		}
	}
	set("X-Agent-ID", bc.AgentID)
	set("X-User-ID", bc.UserID)
	set("X-Channel", bc.Channel)
	set("X-Chat-ID", bc.ChatID)
	set("X-Peer-Kind", bc.PeerKind)
	set("X-Workspace", bc.Workspace)
	set("X-Tenant-ID", bc.TenantID)
	set("X-Local-Key", bc.LocalKey)
	set("X-Session-Key", sessionKey)
	set("X-Delegation-ID", bc.DelegationID)
	set("X-Delegation-Inputs", bc.DelegationInputs)
	if gatewayToken != "" && (bc.AgentID != "" || bc.UserID != "") {
		extra := []string{bc.LocalKey, sessionKey}
		if bc.DelegationID != "" || bc.DelegationInputs != "" {
			extra = append(extra, bc.DelegationID, bc.DelegationInputs)
		}
		headers["X-Bridge-Sig"] = SignBridgeContext(
			gatewayToken,
			bc.AgentID,
			bc.UserID,
			bc.Channel,
			bc.ChatID,
			bc.PeerKind,
			bc.Workspace,
			bc.TenantID,
			extra...,
		)
	}
	return headers
}
