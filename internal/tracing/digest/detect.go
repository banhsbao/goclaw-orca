package digest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type traceView struct {
	input       *Input
	agent       string
	parentAgent string
	llmCalls    int
	maxPrompt   int
	sumPrompt   int
	prompts     int
	toolErrors  int
}

func newTraceView(in *Input, agent string) *traceView {
	v := &traceView{input: in, agent: agent}
	for _, sp := range in.Spans {
		switch sp.SpanType {
		case store.SpanTypeLLMCall:
			v.llmCalls++
			v.prompts++
			v.sumPrompt += sp.InputTokens
			v.maxPrompt = max(v.maxPrompt, sp.InputTokens)
		case store.SpanTypeToolCall:
			if sp.Status == store.SpanStatusError {
				v.toolErrors++
			}
		}
	}
	if v.llmCalls == 0 {
		v.llmCalls = in.Trace.LLMCallCount
	}
	return v
}

func detectTrace(v *traceView, opts Options, acc *findingSet) {
	traceID := v.input.Trace.ID.String()
	spans := append([]store.SpanData(nil), v.input.Spans...)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].StartTime.Before(spans[j].StartTime) })

	calls := map[string]int{}
	callExample := map[string]store.SpanData{}
	for _, sp := range spans {
		switch sp.SpanType {
		case store.SpanTypeToolCall:
			tool := firstNonEmpty(sp.ToolName, sp.Name)
			if sp.Status == store.SpanStatusError {
				text := spanErrorText(sp.Error, sp.OutputPreview)
				class, fix := classifyError(text)
				acc.add(Finding{
					Kind:    KindToolError,
					Agent:   v.agent,
					Tool:    tool,
					Class:   class,
					FixHint: fix,
					Message: normalizeMessage(text, 180),
					Example: truncate(sp.InputPreview, 160),
				}, traceID, 0)
			}
			key := tool + "\x00" + normalizeMessage(sp.InputPreview, 300)
			calls[key]++
			callExample[key] = sp
		case store.SpanTypeLLMCall:
			if sp.FinishReason == "length" {
				acc.add(Finding{
					Kind:    KindTruncatedOutput,
					Agent:   v.agent,
					FixHint: FixConfig,
					Message: "LLM response cut by max_tokens (finish_reason=length)",
				}, traceID, 0)
			}
		}
	}

	for key, n := range calls {
		if n < opts.RepeatedCallMin {
			continue
		}
		sp := callExample[key]
		tool := firstNonEmpty(sp.ToolName, sp.Name)
		acc.add(Finding{
			Kind:    KindRepeatedCall,
			Agent:   v.agent,
			Tool:    tool,
			FixHint: FixPrompt,
			Message: fmt.Sprintf("%s called repeatedly with identical input", tool),
			Example: truncate(sp.InputPreview, 160),
		}, traceID, n)
	}

	if v.maxPrompt >= opts.LargePromptTokens {
		acc.add(Finding{
			Kind:    KindLargePrompt,
			Agent:   v.agent,
			FixHint: FixPrompt,
			Message: fmt.Sprintf("prompt reached %d+ tokens in a single LLM call", opts.LargePromptTokens),
		}, traceID, v.maxPrompt)
	}
	if v.llmCalls >= opts.ManyRounds {
		acc.add(Finding{
			Kind:    KindManyRounds,
			Agent:   v.agent,
			FixHint: FixPrompt,
			Message: fmt.Sprintf("%d+ LLM rounds in one trace", opts.ManyRounds),
		}, traceID, v.llmCalls)
	}
	if v.input.Trace.Status == store.TraceStatusError {
		acc.add(Finding{
			Kind:    KindTraceError,
			Agent:   v.agent,
			FixHint: FixReview,
			Message: normalizeMessage(firstNonEmpty(v.input.Trace.Error, "trace ended with error"), 180),
		}, traceID, 0)
	}
}

func detectRedelegation(rootID string, steps []*traceView, acc *findingSet) {
	type pair struct{ from, to string }
	counts := map[pair]int{}
	for _, v := range steps[1:] {
		counts[pair{v.parentAgent, v.agent}]++
	}
	for p, n := range counts {
		if n < 2 {
			continue
		}
		acc.add(Finding{
			Kind:    KindRedelegation,
			Agent:   p.from,
			Tool:    "delegate",
			FixHint: FixPrompt,
			Message: fmt.Sprintf("delegated to %s more than once in one run", p.to),
		}, rootID, n)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
