package digest

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var (
	agentOrch   = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	agentLead   = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	agentEng    = uuid.MustParse("00000000-0000-0000-0000-00000000000c")
	agentRetro  = uuid.MustParse("00000000-0000-0000-0000-00000000000d")
	testKeys    = map[uuid.UUID]string{agentOrch: "orchestrator", agentLead: "tech-lead", agentEng: "software-engineer", agentRetro: "ops-retro"}
	testBaseTim = time.Date(2026, 11, 2, 16, 39, 0, 0, time.UTC)
)

func trace(id string, agent uuid.UUID, parent string, offsetSec int, tokens int) store.TraceData {
	t := store.TraceData{
		ID:               uuid.MustParse(id),
		AgentID:          &agent,
		StartTime:        testBaseTim.Add(time.Duration(offsetSec) * time.Second),
		Status:           store.TraceStatusCompleted,
		TotalInputTokens: tokens,
		InputPreview:     "status of Prism",
	}
	end := t.StartTime.Add(30 * time.Second)
	t.EndTime = &end
	if parent != "" {
		p := uuid.MustParse(parent)
		t.ParentTraceID = &p
	}
	return t
}

func toolSpan(sec int, tool, input, status, errText string) store.SpanData {
	return store.SpanData{
		SpanType:     store.SpanTypeToolCall,
		ToolName:     tool,
		Name:         tool,
		InputPreview: input,
		Status:       status,
		Error:        errText,
		StartTime:    testBaseTim.Add(time.Duration(sec) * time.Second),
	}
}

func llmSpan(sec, prompt int, finish string) store.SpanData {
	return store.SpanData{
		SpanType:     store.SpanTypeLLMCall,
		InputTokens:  prompt,
		FinishReason: finish,
		Status:       store.SpanStatusCompleted,
		StartTime:    testBaseTim.Add(time.Duration(sec) * time.Second),
	}
}

func findingByKind(d Digest, kind, agent string) *Finding {
	for i := range d.Findings {
		if d.Findings[i].Kind == kind && d.Findings[i].Agent == agent {
			return &d.Findings[i]
		}
	}
	return nil
}

func prismRun() []Input {
	const (
		orch = "10000000-0000-0000-0000-000000000001"
		lead = "10000000-0000-0000-0000-000000000002"
		eng1 = "10000000-0000-0000-0000-000000000003"
		eng2 = "10000000-0000-0000-0000-000000000004"
	)
	updateErr := "no updates provided (set description, subject, or blocked_by)"
	return []Input{
		{Trace: trace(eng2, agentEng, lead, 160, 200000), Spans: []store.SpanData{
			llmSpan(161, 45000, "tool_calls"),
			toolSpan(162, "exec", `{"command":"orca --version"}`, store.SpanStatusError,
				"[CREDENTIALED EXEC] Command blocked by security policy. Binary: orca Args: --version Matched deny pattern: ^-"),
		}},
		{Trace: trace(orch, agentOrch, "", 0, 60000), Spans: []store.SpanData{
			llmSpan(1, 12000, "tool_calls"),
			toolSpan(2, "team_tasks", `{"action":"update","type":"note"}`, store.SpanStatusError, updateErr),
			toolSpan(3, "team_tasks", `{"action":"update","type":"note"}`, store.SpanStatusError, updateErr),
			toolSpan(4, "team_tasks", `{"action":"update","type":"note"}`, store.SpanStatusError, updateErr),
		}},
		{Trace: trace(lead, agentLead, orch, 10, 300000), Spans: []store.SpanData{
			llmSpan(11, 30000, "length"),
			toolSpan(12, "write_file", `{"path":"report.md"}`, store.SpanStatusError,
				`...ent-tech-lead-team-01a1881c-f3" is already in use by container "[REDACTED]". You have to remove (or rename)`),
		}},
		{Trace: trace(eng1, agentEng, lead, 20, 300000), Spans: []store.SpanData{
			llmSpan(21, 20000, "tool_calls"),
			toolSpan(22, "exec", `{"command":"orca terminal read --terminal term_1"}`, store.SpanStatusCompleted, ""),
			toolSpan(23, "exec", `{"command":"orca terminal read --terminal term_1"}`, store.SpanStatusCompleted, ""),
			toolSpan(24, "exec", `{"command":"orca terminal read --terminal term_1"}`, store.SpanStatusCompleted, ""),
		}},
	}
}

func TestBuildGroupsDelegationTreeIntoOneRun(t *testing.T) {
	d := Build(prismRun(), Options{AgentKeys: testKeys})
	if d.Totals.Runs != 1 || len(d.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", d.Totals.Runs)
	}
	run := d.Runs[0]
	if run.Steps[0].Agent != "orchestrator" || len(run.Steps) != 4 {
		t.Fatalf("steps = %+v", run.Steps)
	}
	if run.InputTokens != 860000 || d.Totals.ToolErrors != 5 {
		t.Fatalf("run tokens = %d, tool errors = %d", run.InputTokens, d.Totals.ToolErrors)
	}
	if run.DurationMS != 190000 {
		t.Fatalf("duration = %d, want 190000", run.DurationMS)
	}
}

func TestBuildDetectsSessionAntiPatterns(t *testing.T) {
	d := Build(prismRun(), Options{AgentKeys: testKeys})

	upd := findingByKind(d, KindToolError, "orchestrator")
	if upd == nil || upd.Count != 3 || upd.MaxPerTrace != 3 || upd.Severity != SeverityHigh || upd.Class != ClassToolValidation {
		t.Fatalf("orchestrator update loop finding = %+v", upd)
	}
	conflict := findingByKind(d, KindToolError, "tech-lead")
	if conflict == nil || conflict.Class != ClassSandboxConflict || conflict.FixHint != FixEnv {
		t.Fatalf("sandbox conflict finding = %+v", conflict)
	}
	blocked := findingByKind(d, KindToolError, "software-engineer")
	if blocked == nil || blocked.Class != ClassPolicyBlocked {
		t.Fatalf("policy finding = %+v", blocked)
	}
	if f := findingByKind(d, KindTruncatedOutput, "tech-lead"); f == nil || f.Severity != SeverityHigh {
		t.Fatalf("truncated finding = %+v", f)
	}
	if f := findingByKind(d, KindRepeatedCall, "software-engineer"); f == nil || f.Peak != 3 {
		t.Fatalf("repeated call finding = %+v", f)
	}
	if f := findingByKind(d, KindLargePrompt, "software-engineer"); f == nil || f.Peak != 45000 {
		t.Fatalf("large prompt finding = %+v", f)
	}
	redeleg := findingByKind(d, KindRedelegation, "tech-lead")
	if redeleg == nil || redeleg.Peak != 2 {
		t.Fatalf("redelegation finding = %+v", redeleg)
	}
	if d.Findings[0].Severity != SeverityHigh {
		t.Fatalf("findings not sorted by severity: %+v", d.Findings[0])
	}
}

func TestBuildExcludesRunsTouchingExcludedAgent(t *testing.T) {
	inputs := prismRun()
	inputs = append(inputs,
		Input{Trace: trace("20000000-0000-0000-0000-000000000001", agentOrch, "", 400, 40000)},
		Input{Trace: trace("20000000-0000-0000-0000-000000000002", agentRetro, "20000000-0000-0000-0000-000000000001", 401, 90000)},
	)
	d := Build(inputs, Options{AgentKeys: testKeys, ExcludeAgents: []string{"ops-retro"}})
	if d.Totals.Runs != 1 || d.Totals.ExcludedRuns != 1 {
		t.Fatalf("runs = %d excluded = %d", d.Totals.Runs, d.Totals.ExcludedRuns)
	}
	for _, a := range d.Agents {
		if a.Agent == "ops-retro" {
			t.Fatalf("excluded agent counted: %+v", a)
		}
	}
}

func TestBuildKeepsMostExpensiveRunsInStartOrder(t *testing.T) {
	inputs := []Input{
		{Trace: trace("40000000-0000-0000-0000-000000000001", agentOrch, "", 0, 100)},
		{Trace: trace("40000000-0000-0000-0000-000000000002", agentOrch, "", 10, 900)},
		{Trace: trace("40000000-0000-0000-0000-000000000003", agentOrch, "", 20, 500)},
	}
	d := Build(inputs, Options{AgentKeys: testKeys, MaxRuns: 2})
	if d.Totals.Runs != 3 || d.RunsOmitted != 1 || len(d.Runs) != 2 {
		t.Fatalf("runs = %d omitted = %d kept = %d", d.Totals.Runs, d.RunsOmitted, len(d.Runs))
	}
	if d.Runs[0].InputTokens != 900 || d.Runs[1].InputTokens != 500 {
		t.Fatalf("kept runs = %+v", d.Runs)
	}
	if d.Totals.InputTokens != 1500 {
		t.Fatalf("totals must include omitted runs, got %d", d.Totals.InputTokens)
	}
}

func TestBuildTreatsParentOutsideWindowAsRoot(t *testing.T) {
	orphan := trace("30000000-0000-0000-0000-000000000001", agentEng, "30000000-0000-0000-0000-0000000000ff", 0, 1000)
	d := Build([]Input{{Trace: orphan}}, Options{AgentKeys: testKeys})
	if d.Totals.Runs != 1 || d.Runs[0].Steps[0].Agent != "software-engineer" {
		t.Fatalf("orphan run = %+v", d.Runs)
	}
}

func TestNormalizeMessageExtractsJSONErrorCode(t *testing.T) {
	a := normalizeMessage(`483b-4796-a9ae-fb158fcc5aa1",   "ok": false,   "error": {     "code": "repo_not_found",     "message": "repo Prism not found"`, 200)
	b := normalizeMessage(`4ced-437e-bc08-4747477b4acf",   "ok": false,   "error": {     "code": "repo_not_found",     "message": "repo Prism not found"`, 200)
	if a != b || a != "repo_not_found: repo Prism not found" {
		t.Fatalf("got %q and %q", a, b)
	}
}

func TestClassifyErrorIsCaseInsensitive(t *testing.T) {
	class, fix := classifyError(`Binary "orca" requires a secure CLI grant. Ask admin to grant access to this agent.`)
	if class != ClassPolicyBlocked || fix != FixConfig {
		t.Fatalf("class = %s fix = %s", class, fix)
	}
}

func TestNormalizeMessageGroupsVolatileIDs(t *testing.T) {
	a := normalizeMessage(`...container "goclaw-sbx-01a1881c-f31c-7186-8e05-ca89f061ade6" is already in use`, 200)
	b := normalizeMessage(`container "goclaw-sbx-01a19d21-2204-780e-a40e-b541e64c0304" is already in use`, 200)
	if a != b {
		t.Fatalf("normalized messages differ:\n%s\n%s", a, b)
	}
}
