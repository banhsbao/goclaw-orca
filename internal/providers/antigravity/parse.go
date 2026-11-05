package antigravity

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	ansiRe    = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]|\r`)
	authURLRe = regexp.MustCompile(`https://accounts\.google\.com/o/oauth2/[^\s>"']+`)
	modelIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]*$`)
)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func extractAuthURL(s string) string {
	for _, m := range authURLRe.FindAllString(s, -1) {
		u, err := url.Parse(m)
		if err != nil || u.Host != "accounts.google.com" {
			continue
		}
		redirect, err := url.Parse(u.Query().Get("redirect_uri"))
		if err != nil || redirect.Host != "antigravity.google" {
			continue
		}
		return m
	}
	return ""
}

func parseModels(out string) []ModelInfo {
	var models []ModelInfo
	for _, line := range strings.Split(stripANSI(out), "\n") {
		id, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !modelIDRe.MatchString(id) {
			continue
		}
		models = append(models, ModelInfo{ID: id, Name: strings.TrimSpace(name)})
	}
	return models
}
