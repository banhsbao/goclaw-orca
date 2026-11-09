package providers

import "strings"

const acpHistoryReplayMaxChars = 40_000

func buildACPHistoryReplay(msgs []Message) string {
	last := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			last = i
			break
		}
	}
	if last <= 0 {
		return ""
	}

	var turns []string
	total := 0
	for i := last - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		turn := "[" + m.Role + "]\n" + text
		if total+len(turn) > acpHistoryReplayMaxChars {
			break
		}
		total += len(turn)
		turns = append(turns, turn)
	}
	if len(turns) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<conversation_history>\nEarlier turns of this conversation, oldest first. Treat them as context you already know; do not answer them again.\n\n")
	for i := len(turns) - 1; i >= 0; i-- {
		b.WriteString(turns[i])
		b.WriteString("\n\n")
	}
	b.WriteString("</conversation_history>")
	return b.String()
}
