package store

import (
	"strings"
	"testing"
)

func TestValidateOrcaPairingCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"valid", "orca://host:6768#abc", true},
		{"padded", "  orca://host#x  ", true},
		{"empty", "", false},
		{"prefix only", "orca://", false},
		{"wrong scheme", "https://host", false},
		{"inner space", "orca://a b", false},
		{"newline", "orca://a\nb", false},
		{"too long", "orca://" + strings.Repeat("a", OrcaPairingCodeMaxLen), false},
	}
	for _, c := range cases {
		if err := ValidateOrcaPairingCode(c.in); (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestPrepareOrcaServerInput(t *testing.T) {
	srv := &OrcaServer{Label: " Prod-1 ", Description: "a\nb", PairingCode: " orca://h#c "}
	if err := PrepareOrcaServerInput(srv); err != nil {
		t.Fatal(err)
	}
	if srv.Label != "prod-1" || srv.Description != "a b" || srv.PairingCode != "orca://h#c" {
		t.Fatalf("unexpected normalization: %+v", srv)
	}
	for _, label := range []string{"", "default", "Bad Label", strings.Repeat("a", 33)} {
		if err := PrepareOrcaServerInput(&OrcaServer{Label: label, PairingCode: "orca://h#c"}); err == nil {
			t.Errorf("label %q should be rejected", label)
		}
	}
}
