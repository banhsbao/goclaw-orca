package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const traceBriefDefaultWidth = 160

func tracesBriefCmd() *cobra.Command {
	var width int
	var errorsOnly bool
	cmd := &cobra.Command{
		Use:   "brief <trace-id>",
		Short: "Print one compact line per LLM and tool span of a trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			detail, err := loadTraceDetailLarge(args[0])
			if err != nil {
				return err
			}
			keys, err := loadAgentKeys()
			if err != nil {
				return err
			}
			agent := "unknown"
			if id := detail.Trace.AgentID; id != nil {
				agent = firstNonEmpty(keys[*id], id.String())
			}
			return printTraceBrief(os.Stdout, detail, agent, width, errorsOnly)
		},
	}
	cmd.Flags().IntVar(&width, "width", traceBriefDefaultWidth, "maximum characters per input or error preview")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "show only failed tool spans")
	return cmd
}

func printTraceBrief(w io.Writer, detail traceDetailResponse, agent string, width int, errorsOnly bool) error {
	tr := detail.Trace
	fmt.Fprintf(w, "trace %s agent=%s status=%s tokens=%d/%d llm=%d tools=%d\n",
		tr.ID, agent, tr.Status, tr.TotalInputTokens, tr.TotalOutputTokens, tr.LLMCallCount, tr.ToolCallCount)
	if p := oneLine(tr.InputPreview, width); p != "" {
		fmt.Fprintf(w, "request: %s\n", p)
	}
	spans := append([]spanDataForCLI(nil), detail.Spans...)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].StartTime.Before(spans[j].StartTime) })
	for _, sp := range spans {
		ts := sp.StartTime.UTC().Format("15:04:05")
		switch sp.SpanType {
		case store.SpanTypeLLMCall:
			if errorsOnly {
				continue
			}
			fmt.Fprintf(w, "%s llm  in=%d out=%d finish=%s\n", ts, sp.InputTokens, sp.OutputTokens, sp.FinishReason)
		case store.SpanTypeToolCall:
			failed := sp.Status == store.SpanStatusError
			if errorsOnly && !failed {
				continue
			}
			tool := firstNonEmpty(sp.ToolName, sp.Name)
			line := fmt.Sprintf("%s tool %s %s", ts, tool, oneLine(sp.InputPreview, width))
			if failed {
				line += " -> ERROR " + oneLine(firstNonEmpty(sp.Error, sp.OutputPreview), width)
			}
			fmt.Fprintln(w, line)
		}
	}
	return nil
}

func oneLine(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncateStr(s, width)
}
