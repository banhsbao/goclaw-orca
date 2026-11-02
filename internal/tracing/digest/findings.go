package digest

import (
	"slices"
	"sort"
)

const maxTraceIDsPerFinding = 5

type findingEntry struct {
	finding  Finding
	perTrace map[string]int
}

type findingSet struct {
	order   []string
	entries map[string]*findingEntry
}

func newFindingSet() *findingSet {
	return &findingSet{entries: map[string]*findingEntry{}}
}

func (s *findingSet) add(f Finding, traceID string, peak int) {
	key := f.Kind + "\x00" + f.Agent + "\x00" + f.Tool + "\x00" + f.Class + "\x00" + f.Message
	e, ok := s.entries[key]
	if !ok {
		e = &findingEntry{finding: f, perTrace: map[string]int{}}
		s.entries[key] = e
		s.order = append(s.order, key)
	}
	e.finding.Count++
	e.perTrace[traceID]++
	e.finding.MaxPerTrace = max(e.finding.MaxPerTrace, e.perTrace[traceID])
	e.finding.Peak = max(e.finding.Peak, peak)
	if e.finding.Example == "" {
		e.finding.Example = f.Example
	}
	if !slices.Contains(e.finding.TraceIDs, traceID) && len(e.finding.TraceIDs) < maxTraceIDsPerFinding {
		e.finding.TraceIDs = append(e.finding.TraceIDs, traceID)
	}
}

func (s *findingSet) list(limit int) []Finding {
	out := make([]Finding, 0, len(s.order))
	for _, key := range s.order {
		f := s.entries[key].finding
		f.Severity = severityOf(f)
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func severityOf(f Finding) string {
	switch f.Kind {
	case KindTraceError, KindTruncatedOutput:
		return SeverityHigh
	case KindToolError:
		switch {
		case f.Class == ClassSandboxConflict, f.Class == ClassPolicyBlocked, f.MaxPerTrace >= 3:
			return SeverityHigh
		case f.MaxPerTrace == 2, f.Count >= 3:
			return SeverityMedium
		default:
			return SeverityLow
		}
	case KindRepeatedCall:
		if f.Peak >= 5 {
			return SeverityHigh
		}
		return SeverityMedium
	default:
		return SeverityMedium
	}
}

func severityRank(s string) int {
	switch s {
	case SeverityHigh:
		return 0
	case SeverityMedium:
		return 1
	default:
		return 2
	}
}
