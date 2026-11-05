package antigravity

import "testing"

const goodURL = "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc&redirect_uri=https%3A%2F%2Fantigravity.google%2Foauth-callback&response_type=code&state=xyz"

func TestStripANSI(t *testing.T) {
	in := "\x1b[2J\x1b[1;32mSelect login method\x1b[0m\r\n\x1b]0;title\x07done"
	if got := stripANSI(in); got != "Select login method\ndone" {
		t.Errorf("got %q", got)
	}
}

func TestExtractAuthURL(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"valid", "Open this URL:\n" + goodURL + "\npaste the authorization code below", goodURL},
		{"none", "no url here", ""},
		{"foreign host", "https://accounts.google.com.evil.test/o/oauth2/v2/auth?redirect_uri=https%3A%2F%2Fantigravity.google%2Fx", ""},
		{"foreign redirect", "https://accounts.google.com/o/oauth2/v2/auth?redirect_uri=https%3A%2F%2Fevil.test%2Fcb", ""},
		{"missing redirect", "https://accounts.google.com/o/oauth2/v2/auth?client_id=abc", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractAuthURL(c.in); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestParseModels(t *testing.T) {
	out := "Fetching available models...\ngemini-4-argon-medium\tGemini 4 Argon (Medium)\ngpt-oss-120b-medium\tGPT-OSS 120B (Medium)\n\nBad Line\tx\n"
	got := parseModels(out)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].ID != "gemini-4-argon-medium" || got[0].Name != "Gemini 4 Argon (Medium)" {
		t.Errorf("got %+v", got[0])
	}
}

func TestParseModels_Unauthenticated(t *testing.T) {
	if got := parseModels("Please sign in\nhttps://accounts.google.com/x\n"); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestValidCode(t *testing.T) {
	bad := []string{"", "a b", "a\nb", "a\x00b", "a\x1bb", string(make([]byte, 513))}
	for _, c := range bad {
		if validCode(c) {
			t.Errorf("accepted %q", c)
		}
	}
	if !validCode("4/0AbCdEf-ghi_JKL") {
		t.Error("rejected valid code")
	}
}
