package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunTracesDigestJSONOutput(t *testing.T) {
	const (
		agentID  = "22222222-2222-2222-2222-222222222222"
		traceID  = "11111111-1111-1111-1111-111111111111"
		traceRaw = `{"id":"` + traceID + `","agent_id":"` + agentID + `","start_time":"2026-11-02T16:00:00Z","end_time":"2026-11-02T16:00:30Z","status":"completed","total_input_tokens":500,"tool_call_count":1,"created_at":"2026-11-02T16:00:00Z"}`
	)
	var listQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/traces":
			listQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"traces":[` + traceRaw + `],"total":1}`))
		case "/v1/traces/" + traceID:
			_, _ = w.Write([]byte(`{"trace":` + traceRaw + `,"spans":[
				{"id":"33333333-3333-3333-3333-333333333333","trace_id":"` + traceID + `","span_type":"tool_call","tool_name":"exec","status":"error",
				 "error":"Binary \"orca\" requires a secure CLI grant.","start_time":"2026-11-02T16:00:01Z","created_at":"2026-11-02T16:00:01Z"}
			]}`))
		case "/v1/agents":
			_, _ = w.Write([]byte(`{"agents":[{"id":"` + agentID + `","agent_key":"software-engineer"}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	withTraceTestGateway(t, srv)
	gatewayOutputFormat = "json"

	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	out, err := captureStdout(t, func() error {
		return runTracesDigest(traceDigestOptions{Since: "12h", MaxTraces: 10}, now)
	})
	if err != nil {
		t.Fatalf("runTracesDigest: %v", err)
	}
	if !strings.Contains(listQuery, "from=2026-11-02T12%3A00%3A00Z") || !strings.Contains(listQuery, "to=2026-11-03T00%3A00%3A00Z") {
		t.Fatalf("list query = %q", listQuery)
	}
	var got struct {
		Totals struct {
			Runs       int `json:"runs"`
			ToolErrors int `json:"tool_errors"`
		} `json:"totals"`
		Findings []struct {
			Agent string `json:"agent"`
			Class string `json:"class"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if got.Totals.Runs != 1 || got.Totals.ToolErrors != 1 {
		t.Fatalf("totals = %+v", got.Totals)
	}
	if len(got.Findings) != 1 || got.Findings[0].Agent != "software-engineer" || got.Findings[0].Class != "policy_blocked" {
		t.Fatalf("findings = %+v", got.Findings)
	}
}

func TestParseDigestTime(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if got, err := parseDigestTime("24h", now); err != nil || !got.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("duration parse = %v, %v", got, err)
	}
	if got, err := parseDigestTime("2026-11-02T13:00:00+07:00", now); err != nil || got.Hour() != 6 {
		t.Fatalf("rfc3339 parse = %v, %v", got, err)
	}
	if _, err := parseDigestTime("yesterday", now); err == nil {
		t.Fatal("expected error for invalid value")
	}
}
