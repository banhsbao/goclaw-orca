package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintTraceBriefShowsCompactSpans(t *testing.T) {
	raw := `{"trace":{"id":"11111111-1111-1111-1111-111111111111","status":"completed","total_input_tokens":900,"llm_call_count":1,"tool_call_count":2,
		"input_preview":"status\nof Prism","start_time":"2026-11-02T16:00:00Z","created_at":"2026-11-02T16:00:00Z"},
		"spans":[
		{"id":"22222222-2222-2222-2222-222222222222","trace_id":"11111111-1111-1111-1111-111111111111","span_type":"tool_call","tool_name":"exec","status":"error",
		 "input_preview":"{\"command\":\"orca --version\"}","error":"blocked by security policy","start_time":"2026-11-02T16:00:05Z","created_at":"2026-11-02T16:00:05Z"},
		{"id":"33333333-3333-3333-3333-333333333333","trace_id":"11111111-1111-1111-1111-111111111111","span_type":"llm_call","status":"completed",
		 "input_tokens":800,"output_tokens":20,"finish_reason":"tool_calls","start_time":"2026-11-02T16:00:01Z","created_at":"2026-11-02T16:00:01Z"},
		{"id":"44444444-4444-4444-4444-444444444444","trace_id":"11111111-1111-1111-1111-111111111111","span_type":"tool_call","tool_name":"datetime","status":"completed",
		 "input_preview":"{}","start_time":"2026-11-02T16:00:06Z","created_at":"2026-11-02T16:00:06Z"}
		]}`
	var detail traceDetailResponse
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var buf bytes.Buffer
	if err := printTraceBrief(&buf, detail, "software-engineer", 160, false); err != nil {
		t.Fatalf("printTraceBrief: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 || !strings.Contains(lines[0], "agent=software-engineer") || lines[1] != "request: status of Prism" {
		t.Fatalf("output:\n%s", buf.String())
	}
	if !strings.HasPrefix(lines[2], "16:00:01 llm  in=800") || !strings.Contains(lines[3], "-> ERROR blocked by security policy") {
		t.Fatalf("span order or error line wrong:\n%s", buf.String())
	}

	buf.Reset()
	if err := printTraceBrief(&buf, detail, "software-engineer", 160, true); err != nil {
		t.Fatalf("printTraceBrief errors-only: %v", err)
	}
	if strings.Contains(buf.String(), "llm ") || strings.Contains(buf.String(), "datetime") {
		t.Fatalf("errors-only output:\n%s", buf.String())
	}
}
