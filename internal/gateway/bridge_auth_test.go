package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestBridgeAuthMiddleware(t *testing.T) {
	const token = "gw-secret"
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := bridgeAuthMiddleware(token, bridgeContextMiddleware(token, nil, ok))
	bc := providers.BridgeContext{AgentID: "agent-1", UserID: "u1", Channel: "telegram", ChatID: "42"}

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no auth", map[string]string{}, http.StatusUnauthorized},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"bearer", map[string]string{"Authorization": "Bearer " + token}, http.StatusOK},
		{"signed context without bearer", providers.BridgeHeaders(token, "sess", bc, false), http.StatusOK},
		{"forged signature", func() map[string]string {
			m := providers.BridgeHeaders(token, "sess", bc, false)
			m["X-Bridge-Sig"] = "deadbeef"
			return m
		}(), http.StatusForbidden},
		{"tampered context", func() map[string]string {
			m := providers.BridgeHeaders(token, "sess", bc, false)
			m["X-Agent-ID"] = "agent-2"
			return m
		}(), http.StatusForbidden},
		{"signature without agent context", map[string]string{"X-Bridge-Sig": "x"}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}
