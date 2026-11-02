package digest

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultLargePromptTokens = 40000
	defaultManyRounds        = 12
	defaultRepeatedCallMin   = 3
	defaultMaxFindings       = 25
	defaultMaxRuns           = 12
	maxParentDepth           = 32
)

func Build(inputs []Input, opts Options) Digest {
	opts = withDefaults(opts)
	sorted := make([]*Input, len(inputs))
	for i := range inputs {
		sorted[i] = &inputs[i]
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Trace.StartTime.Before(sorted[j].Trace.StartTime) })

	byID := make(map[uuid.UUID]*Input, len(sorted))
	for _, in := range sorted {
		byID[in.Trace.ID] = in
	}

	var rootOrder []uuid.UUID
	groups := map[uuid.UUID][]*Input{}
	for _, in := range sorted {
		root := rootOf(in, byID)
		if _, ok := groups[root]; !ok {
			rootOrder = append(rootOrder, root)
		}
		groups[root] = append(groups[root], in)
	}

	d := Digest{From: opts.From, To: opts.To}
	agents := map[string]*AgentStats{}
	promptSums := map[string][2]int{}
	acc := newFindingSet()

	for _, root := range rootOrder {
		views := make([]*traceView, 0, len(groups[root]))
		agentOf := map[uuid.UUID]string{}
		excluded := false
		for _, in := range groups[root] {
			agent := agentName(in.Trace.AgentID, opts.AgentKeys)
			agentOf[in.Trace.ID] = agent
			excluded = excluded || slices.Contains(opts.ExcludeAgents, agent)
			views = append(views, newTraceView(in, agent))
		}
		if excluded {
			d.Totals.ExcludedRuns++
			continue
		}
		for _, v := range views {
			if p := v.input.Trace.ParentTraceID; p != nil {
				v.parentAgent = agentOf[*p]
			}
		}
		run := buildRun(views)
		for _, v := range views {
			addAgentStats(agents, promptSums, v)
			addTotals(&d.Totals, v)
			detectTrace(v, opts, acc)
		}
		detectRedelegation(run.RootTraceID, views, acc)
		d.Runs = append(d.Runs, run)
	}

	d.Totals.Runs = len(d.Runs)
	for name, st := range agents {
		if s := promptSums[name]; s[1] > 0 {
			st.AvgPromptTokens = s[0] / s[1]
		}
		d.Agents = append(d.Agents, *st)
	}
	sort.SliceStable(d.Agents, func(i, j int) bool { return d.Agents[i].InputTokens > d.Agents[j].InputTokens })
	d.Findings = acc.list(opts.MaxFindings)
	d.Runs, d.RunsOmitted = topRuns(d.Runs, opts.MaxRuns)
	return d
}

func topRuns(runs []Run, limit int) ([]Run, int) {
	if len(runs) <= limit {
		return runs, 0
	}
	kept := append([]Run(nil), runs...)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].InputTokens > kept[j].InputTokens })
	kept = kept[:limit]
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Start.Before(kept[j].Start) })
	return kept, len(runs) - limit
}

func withDefaults(opts Options) Options {
	if opts.LargePromptTokens <= 0 {
		opts.LargePromptTokens = defaultLargePromptTokens
	}
	if opts.ManyRounds <= 0 {
		opts.ManyRounds = defaultManyRounds
	}
	if opts.RepeatedCallMin <= 0 {
		opts.RepeatedCallMin = defaultRepeatedCallMin
	}
	if opts.MaxFindings <= 0 {
		opts.MaxFindings = defaultMaxFindings
	}
	if opts.MaxRuns <= 0 {
		opts.MaxRuns = defaultMaxRuns
	}
	return opts
}

func rootOf(in *Input, byID map[uuid.UUID]*Input) uuid.UUID {
	cur := in
	for range maxParentDepth {
		p := cur.Trace.ParentTraceID
		if p == nil || *p == cur.Trace.ID {
			break
		}
		parent, ok := byID[*p]
		if !ok {
			break
		}
		cur = parent
	}
	return cur.Trace.ID
}

func agentName(id *uuid.UUID, keys map[uuid.UUID]string) string {
	if id == nil {
		return "unknown"
	}
	if k, ok := keys[*id]; ok && k != "" {
		return k
	}
	return id.String()[:8]
}

func traceDurationMS(t store.TraceData) int {
	if t.DurationMS > 0 {
		return t.DurationMS
	}
	if t.EndTime != nil {
		return int(t.EndTime.Sub(t.StartTime).Milliseconds())
	}
	return 0
}

func buildRun(views []*traceView) Run {
	root := views[0].input.Trace
	run := Run{
		RootTraceID: root.ID.String(),
		Start:       root.StartTime,
		Request:     truncate(root.InputPreview, 240),
		Status:      root.Status,
	}
	end := root.StartTime
	for _, v := range views {
		t := v.input.Trace
		step := Step{
			TraceID:         t.ID.String(),
			Agent:           v.agent,
			Status:          t.Status,
			DurationMS:      traceDurationMS(t),
			LLMCalls:        v.llmCalls,
			ToolCalls:       t.ToolCallCount,
			ToolErrors:      v.toolErrors,
			InputTokens:     t.TotalInputTokens,
			MaxPromptTokens: v.maxPrompt,
		}
		if t.EndTime != nil && t.EndTime.After(end) {
			end = *t.EndTime
		}
		if t.Status == store.TraceStatusError {
			run.Status = store.TraceStatusError
		}
		run.InputTokens += t.TotalInputTokens
		run.OutputTokens += t.TotalOutputTokens
		run.Cost += t.TotalCost
		run.ToolErrors += v.toolErrors
		run.Steps = append(run.Steps, step)
	}
	run.DurationMS = int(end.Sub(root.StartTime).Milliseconds())
	return run
}

func addAgentStats(agents map[string]*AgentStats, sums map[string][2]int, v *traceView) {
	st, ok := agents[v.agent]
	if !ok {
		st = &AgentStats{Agent: v.agent}
		agents[v.agent] = st
	}
	t := v.input.Trace
	st.Traces++
	st.LLMCalls += v.llmCalls
	st.ToolCalls += t.ToolCallCount
	st.ToolErrors += v.toolErrors
	st.InputTokens += t.TotalInputTokens
	st.OutputTokens += t.TotalOutputTokens
	st.Cost += t.TotalCost
	st.DurationMS += traceDurationMS(t)
	st.MaxPromptTokens = max(st.MaxPromptTokens, v.maxPrompt)
	s := sums[v.agent]
	sums[v.agent] = [2]int{s[0] + v.sumPrompt, s[1] + v.prompts}
}

func addTotals(t *Totals, v *traceView) {
	tr := v.input.Trace
	t.Traces++
	t.InputTokens += tr.TotalInputTokens
	t.OutputTokens += tr.TotalOutputTokens
	t.CacheReadTokens += cacheReadTokens(tr.Metadata)
	t.Cost += tr.TotalCost
	t.LLMCalls += v.llmCalls
	t.ToolCalls += tr.ToolCallCount
	t.ToolErrors += v.toolErrors
}

func cacheReadTokens(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var meta struct {
		TotalCacheReadTokens int `json:"total_cache_read_tokens"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0
	}
	return meta.TotalCacheReadTokens
}
