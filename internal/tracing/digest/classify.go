package digest

import (
	"regexp"
	"strings"
)

const (
	ClassPolicyBlocked   = "policy_blocked"
	ClassSandboxConflict = "sandbox_conflict"
	ClassUnknownCommand  = "unknown_command"
	ClassScopeDenied     = "scope_denied"
	ClassToolValidation  = "tool_validation"
	ClassSandboxFS       = "sandbox_fs"
	ClassTimeout         = "timeout"
	ClassOther           = "other"
)

const (
	FixPrompt = "prompt"
	FixConfig = "config"
	FixEnv    = "env"
	FixReview = "review"
)

type classRule struct {
	class   string
	fix     string
	needles []string
}

var classRules = []classRule{
	{ClassSandboxConflict, FixEnv, []string{"is already in use by container"}},
	{ClassPolicyBlocked, FixConfig, []string{"requires a secure CLI grant", "blocked by security policy", "blocked by policy"}},
	{ClassUnknownCommand, FixPrompt, []string{"unknown command", "did you mean", "valid flags:", "unknown flag", "command not found", "no such command"}},
	{ClassScopeDenied, FixConfig, []string{"not accessible in current scope", "permission denied"}},
	{ClassTimeout, FixPrompt, []string{`"code": "timeout"`, "timed out", "deadline exceeded"}},
	{ClassSandboxFS, FixPrompt, []string{"is a directory", "no such file or directory", "minimal filesystem"}},
	{ClassToolValidation, FixPrompt, []string{"is required", "no updates provided", "invalid argument", "invalid_argument", "must be"}},
}

func classifyError(text string) (string, string) {
	lower := strings.ToLower(text)
	for _, rule := range classRules {
		for _, needle := range rule.needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				return rule.class, rule.fix
			}
		}
	}
	return ClassOther, FixReview
}

var (
	uuidPattern      = regexp.MustCompile(`[0-9a-fA-F]{4,}(?:-[0-9a-fA-F]{4,}){2,}`)
	jsonErrorPattern = regexp.MustCompile(`"code":\s*"([^"]+)",\s*"message":\s*"([^"]*)"`)
	hexPattern       = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	numberPattern    = regexp.MustCompile(`\b\d+\b`)
	spacePattern     = regexp.MustCompile(`\s+`)
)

func normalizeMessage(text string, limit int) string {
	s := strings.TrimSpace(text)
	if m := jsonErrorPattern.FindStringSubmatch(s); m != nil {
		s = m[1] + ": " + m[2]
	}
	s = strings.TrimPrefix(s, "...")
	s = uuidPattern.ReplaceAllString(s, "<id>")
	s = hexPattern.ReplaceAllString(s, "<hex>")
	s = numberPattern.ReplaceAllString(s, "<n>")
	s = spacePattern.ReplaceAllString(s, " ")
	return truncate(strings.TrimSpace(s), limit)
}

func truncate(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

func spanErrorText(errText, output string) string {
	if strings.TrimSpace(errText) != "" {
		return errText
	}
	return output
}
