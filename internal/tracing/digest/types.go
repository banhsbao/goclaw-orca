package digest

import (
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type Input struct {
	Trace store.TraceData
	Spans []store.SpanData
}

type Options struct {
	From              time.Time
	To                time.Time
	AgentKeys         map[uuid.UUID]string
	ExcludeAgents     []string
	LargePromptTokens int
	ManyRounds        int
	RepeatedCallMin   int
	MaxFindings       int
	MaxRuns           int
}

type Digest struct {
	From        time.Time    `json:"from"`
	To          time.Time    `json:"to"`
	Totals      Totals       `json:"totals"`
	Agents      []AgentStats `json:"agents"`
	Findings    []Finding    `json:"findings"`
	RunsOmitted int          `json:"runs_omitted,omitempty"`
	Runs        []Run        `json:"runs"`
}

type Totals struct {
	Runs            int     `json:"runs"`
	Traces          int     `json:"traces"`
	ExcludedRuns    int     `json:"excluded_runs"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_tokens"`
	Cost            float64 `json:"cost"`
	LLMCalls        int     `json:"llm_calls"`
	ToolCalls       int     `json:"tool_calls"`
	ToolErrors      int     `json:"tool_errors"`
}

type AgentStats struct {
	Agent           string  `json:"agent"`
	Traces          int     `json:"traces"`
	LLMCalls        int     `json:"llm_calls"`
	ToolCalls       int     `json:"tool_calls"`
	ToolErrors      int     `json:"tool_errors"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	Cost            float64 `json:"cost"`
	AvgPromptTokens int     `json:"avg_prompt_tokens"`
	MaxPromptTokens int     `json:"max_prompt_tokens"`
	DurationMS      int     `json:"duration_ms"`
}

type Run struct {
	RootTraceID  string    `json:"root_trace_id"`
	Start        time.Time `json:"start"`
	DurationMS   int       `json:"duration_ms"`
	Request      string    `json:"request,omitempty"`
	Status       string    `json:"status"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Cost         float64   `json:"cost"`
	ToolErrors   int       `json:"tool_errors"`
	Steps        []Step    `json:"steps"`
}

type Step struct {
	TraceID         string `json:"trace_id"`
	Agent           string `json:"agent"`
	Status          string `json:"status"`
	DurationMS      int    `json:"duration_ms"`
	LLMCalls        int    `json:"llm_calls"`
	ToolCalls       int    `json:"tool_calls"`
	ToolErrors      int    `json:"tool_errors"`
	InputTokens     int    `json:"input_tokens"`
	MaxPromptTokens int    `json:"max_prompt_tokens"`
}

const (
	KindToolError       = "tool_error"
	KindRepeatedCall    = "repeated_call"
	KindTruncatedOutput = "truncated_output"
	KindLargePrompt     = "large_prompt"
	KindManyRounds      = "many_rounds"
	KindRedelegation    = "redelegation"
	KindTraceError      = "trace_error"
)

const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"
)

type Finding struct {
	Kind        string   `json:"kind"`
	Severity    string   `json:"severity"`
	Agent       string   `json:"agent"`
	Tool        string   `json:"tool,omitempty"`
	Class       string   `json:"class,omitempty"`
	FixHint     string   `json:"fix_hint"`
	Count       int      `json:"count"`
	MaxPerTrace int      `json:"max_per_trace"`
	Peak        int      `json:"peak,omitempty"`
	Message     string   `json:"message"`
	Example     string   `json:"example,omitempty"`
	TraceIDs    []string `json:"trace_ids"`
}
