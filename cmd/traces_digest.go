package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/tracing/digest"
)

const (
	traceDigestPageSize      = 200
	traceDigestDefaultTraces = 500
)

type traceDigestOptions struct {
	Since         string
	Until         string
	ExcludeAgents []string
	LargePrompt   int
	ManyRounds    int
	RepeatMin     int
	MaxFindings   int
	MaxRuns       int
	MaxTraces     int
}

type agentKeyListResponse struct {
	Agents []struct {
		ID       uuid.UUID `json:"id"`
		AgentKey string    `json:"agent_key"`
	} `json:"agents"`
}

func tracesDigestCmd() *cobra.Command {
	var opts traceDigestOptions
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Summarize runs, per-agent cost, and failure patterns for a time window",
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesDigest(opts, time.Now().UTC())
		},
	}
	cmd.Flags().StringVar(&opts.Since, "since", "24h", "window start: duration back from now (e.g. 24h) or RFC3339")
	cmd.Flags().StringVar(&opts.Until, "until", "", "window end: duration back from now or RFC3339 (default now)")
	cmd.Flags().StringSliceVar(&opts.ExcludeAgents, "exclude-agent", nil, "agent keys whose runs are skipped")
	cmd.Flags().IntVar(&opts.LargePrompt, "large-prompt", 0, "flag LLM calls whose prompt reaches this many tokens (default 40000)")
	cmd.Flags().IntVar(&opts.ManyRounds, "many-rounds", 0, "flag traces with at least this many LLM rounds (default 12)")
	cmd.Flags().IntVar(&opts.RepeatMin, "repeat-min", 0, "flag identical tool calls repeated this many times in a trace (default 3)")
	cmd.Flags().IntVar(&opts.MaxFindings, "max-findings", 0, "maximum findings to report (default 25)")
	cmd.Flags().IntVar(&opts.MaxRuns, "max-runs", 0, "maximum runs listed, most expensive first; totals still cover every run (default 12)")
	cmd.Flags().IntVar(&opts.MaxTraces, "max-traces", traceDigestDefaultTraces, "maximum traces to load")
	return cmd
}

func runTracesDigest(opts traceDigestOptions, now time.Time) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	from, err := parseDigestTime(opts.Since, now)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}
	to := now
	if opts.Until != "" {
		if to, err = parseDigestTime(opts.Until, now); err != nil {
			return fmt.Errorf("--until: %w", err)
		}
	}
	if !from.Before(to) {
		return fmt.Errorf("--since must be before --until")
	}

	inputs, err := loadDigestInputs(from, to, opts.MaxTraces)
	if err != nil {
		return err
	}
	keys, err := loadAgentKeys()
	if err != nil {
		return err
	}
	d := digest.Build(inputs, digest.Options{
		From:              from,
		To:                to,
		AgentKeys:         keys,
		ExcludeAgents:     opts.ExcludeAgents,
		LargePromptTokens: opts.LargePrompt,
		ManyRounds:        opts.ManyRounds,
		RepeatedCallMin:   opts.RepeatMin,
		MaxFindings:       opts.MaxFindings,
		MaxRuns:           opts.MaxRuns,
	})
	if outputFormatIsJSON() {
		return json.NewEncoder(os.Stdout).Encode(d)
	}
	return printTraceDigest(d)
}

func parseDigestTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if dur, err := time.ParseDuration(value); err == nil {
		return now.Add(-dur), nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is neither a duration nor RFC3339", value)
	}
	return t.UTC(), nil
}

func loadDigestInputs(from, to time.Time, maxTraces int) ([]digest.Input, error) {
	if maxTraces <= 0 {
		maxTraces = traceDigestDefaultTraces
	}
	var traces []traceDataForCLI
	for offset := 0; len(traces) < maxTraces; offset += traceDigestPageSize {
		page, err := gatewayHTTPGetTyped[traceListResponse](buildTraceListPath(traceListOptions{
			From:   from.Format(time.RFC3339),
			To:     to.Format(time.RFC3339),
			Limit:  traceDigestPageSize,
			Offset: offset,
		}))
		if err != nil {
			return nil, err
		}
		traces = append(traces, page.Traces...)
		if len(page.Traces) < traceDigestPageSize {
			break
		}
	}
	if len(traces) > maxTraces {
		traces = traces[:maxTraces]
	}
	inputs := make([]digest.Input, 0, len(traces))
	for _, tr := range traces {
		detail, err := loadTraceDetailLarge(tr.ID.String())
		if err != nil {
			return nil, fmt.Errorf("trace %s: %w", tr.ID, err)
		}
		inputs = append(inputs, digest.Input{Trace: detail.Trace, Spans: detail.Spans})
	}
	return inputs, nil
}

func loadTraceDetailLarge(traceID string) (traceDetailResponse, error) {
	var detail traceDetailResponse
	raw, status, err := gatewayHTTPDoRawWithLimit(http.MethodGet, traceDetailPath(traceID), nil, traceExportResponseLimit)
	if err != nil {
		return detail, err
	}
	if status >= 400 {
		return detail, parseHTTPError(raw, status)
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return detail, fmt.Errorf("decode trace detail: %w", err)
	}
	return detail, nil
}

func loadAgentKeys() (map[uuid.UUID]string, error) {
	resp, err := gatewayHTTPGetTyped[agentKeyListResponse]("/v1/agents")
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	keys := make(map[uuid.UUID]string, len(resp.Agents))
	for _, a := range resp.Agents {
		keys[a.ID] = a.AgentKey
	}
	return keys, nil
}

func printTraceDigest(d digest.Digest) error {
	t := d.Totals
	fmt.Printf("Window: %s → %s\n", formatTraceTime(d.From), formatTraceTime(d.To))
	fmt.Printf("Runs: %d (excluded %d)  Traces: %d  Tokens: %d in / %d out (cache read %d)  Cost: $%.4f  Tool errors: %d/%d\n\n",
		t.Runs, t.ExcludedRuns, t.Traces, t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.Cost, t.ToolErrors, t.ToolCalls)
	if t.Runs == 0 {
		fmt.Println("No runs in window.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tTRACES\tLLM\tTOOLS\tERRORS\tINPUT\tAVG PROMPT\tMAX PROMPT\tCOST")
	for _, a := range d.Agents {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t$%.4f\n",
			a.Agent, a.Traces, a.LLMCalls, a.ToolCalls, a.ToolErrors, a.InputTokens, a.AvgPromptTokens, a.MaxPromptTokens, a.Cost)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(d.Findings) == 0 {
		fmt.Println("\nNo findings.")
		return nil
	}
	fmt.Println()
	tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tKIND\tAGENT\tTOOL\tCLASS\tFIX\tCOUNT\tMAX/TRACE\tPEAK\tMESSAGE")
	for _, f := range d.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			f.Severity, f.Kind, f.Agent, f.Tool, f.Class, f.FixHint, f.Count, f.MaxPerTrace, f.Peak, truncateStr(f.Message, 90))
	}
	return tw.Flush()
}
