package providers

import (
	"strings"
	"testing"
)

func TestBuildACPHistoryReplay_IncludesPriorTurnsOldestFirst(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "clone Luminoid/Prism"},
		{Role: "assistant", Content: "Prism running on simulator"},
		{Role: "tool", Content: "tool output"},
		{Role: "user", Content: "capture flows"},
	}
	got := buildACPHistoryReplay(msgs)
	first := strings.Index(got, "clone Luminoid/Prism")
	second := strings.Index(got, "Prism running on simulator")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("prior turns missing or out of order: %q", got)
	}
	for _, unwanted := range []string{"sys", "tool output", "capture flows"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("replay must not contain %q: %q", unwanted, got)
		}
	}
}

func TestBuildACPHistoryReplay_EmptyForFirstTurn(t *testing.T) {
	msgs := []Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}
	if got := buildACPHistoryReplay(msgs); got != "" {
		t.Fatalf("expected empty replay, got %q", got)
	}
}

func TestBuildACPHistoryReplay_KeepsNewestWithinBudget(t *testing.T) {
	big := strings.Repeat("x", acpHistoryReplayMaxChars)
	msgs := []Message{
		{Role: "user", Content: big},
		{Role: "assistant", Content: "recent answer"},
		{Role: "user", Content: "now"},
	}
	got := buildACPHistoryReplay(msgs)
	if !strings.Contains(got, "recent answer") || strings.Contains(got, big) {
		t.Fatalf("budget should drop oldest turn and keep newest")
	}
}

func TestExtractACPContent_ReplayOnlyWhenFresh(t *testing.T) {
	req := ChatRequest{Messages: []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "clone Luminoid/Prism"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "capture flows"},
	}}
	fresh := extractACPContent(req, true)[0].Text
	if !strings.HasPrefix(fresh, "sys\n\n<conversation_history>") || !strings.HasSuffix(fresh, "capture flows") || !strings.Contains(fresh, "clone Luminoid/Prism") {
		t.Fatalf("fresh session prompt missing history: %q", fresh)
	}
	live := extractACPContent(req, false)[0].Text
	if live != "sys\n\ncapture flows" {
		t.Fatalf("live session prompt should not replay history: %q", live)
	}
}
