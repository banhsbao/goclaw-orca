package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type labeledStubStore struct {
	*stubSecureCLIStore
	mu          sync.Mutex
	labels      []store.SecureCLICredentialLabel
	resolved    map[string]*store.SecureCLIResolvedCredential
	listCalls   int
	listErr     error
	resolveCall []string
}

func (s *labeledStubStore) ListCredentialLabels(ctx context.Context, q store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	return s.labels, s.listErr
}

func (s *labeledStubStore) ResolveLabeledCredential(ctx context.Context, binaryID uuid.UUID, agentID *uuid.UUID, userID, label string) (*store.SecureCLIResolvedCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveCall = append(s.resolveCall, label)
	return s.resolved[label], nil
}

func (s *labeledStubStore) GetUserCredentialByLabel(context.Context, uuid.UUID, string, string) (*store.SecureCLIUserCredential, error) {
	return nil, nil
}
func (s *labeledStubStore) SetUserCredentialLabeled(context.Context, uuid.UUID, string, store.SecureCLICredentialWrite) error {
	return nil
}
func (s *labeledStubStore) DeleteUserCredentialByLabel(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (s *labeledStubStore) GetAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) (*store.SecureCLIAgentCredential, error) {
	return nil, nil
}
func (s *labeledStubStore) SetAgentCredentialLabeled(context.Context, uuid.UUID, uuid.UUID, store.SecureCLICredentialWrite, string) error {
	return nil
}
func (s *labeledStubStore) DeleteAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func TestParseServerSelector(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantLabel string
		wantRest  []string
		wantFound bool
	}{
		{"separate value", []string{"--server", "x", "status"}, "x", []string{"status"}, true},
		{"equals value", []string{"--server=x", "status"}, "x", []string{"status"}, true},
		{"flag alone", []string{"--server"}, "", []string{}, true},
		{"empty equals", []string{"--server=", "status"}, "", []string{"status"}, true},
		{"not first", []string{"status", "--server", "x"}, "", []string{"status", "--server", "x"}, false},
		{"no args", nil, "", nil, false},
		{"similar flag", []string{"--servers", "x"}, "", []string{"--servers", "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label, rest, found := parseServerSelector(tc.args)
			if label != tc.wantLabel || found != tc.wantFound || strings.Join(rest, "\x00") != strings.Join(tc.wantRest, "\x00") {
				t.Fatalf("got (%q,%v,%v), want (%q,%v,%v)", label, rest, found, tc.wantLabel, tc.wantRest, tc.wantFound)
			}
		})
	}
}

func TestDecideServerSelection(t *testing.T) {
	one := []store.SecureCLICredentialLabel{{Label: "build-vm"}}
	two := []store.SecureCLICredentialLabel{{Label: "build-vm"}, {Label: "design-mac"}}
	onlyDefault := []store.SecureCLICredentialLabel{{Label: ""}}
	cases := []struct {
		name       string
		labels     []store.SecureCLICredentialLabel
		requested  string
		hasSel     bool
		hasDefault bool
		want       serverSelection
		wantLabel  string
	}{
		{"no labels passthrough", nil, "x", true, false, serverPassthrough, ""},
		{"only default row passthrough", onlyDefault, "x", true, true, serverPassthrough, ""},
		{"no selector default exists", two, "", false, true, serverPassthrough, ""},
		{"no selector no default one label", one, "", false, false, serverPick, "build-vm"},
		{"no selector no default two labels", two, "", false, false, serverAmbiguous, ""},
		{"selector known", two, "design-mac", true, false, serverPick, "design-mac"},
		{"selector uppercase normalized", two, "Design-Mac", true, true, serverPick, "design-mac"},
		{"selector unknown", two, "other", true, true, serverUnknown, ""},
		{"selector default with default", two, "default", true, true, serverUseDefault, ""},
		{"selector default without default", two, "default", true, false, serverUnknown, ""},
		{"selector empty", two, "", true, true, serverUnknown, ""},
		{"selector flag-like", two, "--pairing-code", true, true, serverUnknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, label := decideServerSelection(tc.labels, tc.requested, tc.hasSel, tc.hasDefault)
			if got != tc.want || label != tc.wantLabel {
				t.Fatalf("got (%v,%q), want (%v,%q)", got, label, tc.want, tc.wantLabel)
			}
		})
	}
}

func TestSelectedLabeledCredentialDoesNotBleedDefaultUserEnv(t *testing.T) {
	patType := "pat"
	host := "github.com"
	bin := &store.SecureCLIBinary{
		EncryptedEnv: []byte(`{"BASE":"b"}`),
	}
	bin.SetEffectiveCredential([]byte(`{"DEFAULT_SECRET":"leak"}`), nil, nil, "user", "u1")

	bin.ApplyLabeledCredential(&store.SecureCLIResolvedCredential{
		Label:          "build-vm",
		Source:         "agent",
		Env:            []byte(`{"TOKEN":"t"}`),
		CredentialType: &patType,
		HostScope:      &host,
	})

	env, err := mergeCredentialedEnv(bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := env["DEFAULT_SECRET"]; ok {
		t.Fatalf("default user env bled into labeled credential: %v", env)
	}
	if env["BASE"] != "b" {
		t.Fatalf("base env lost: %v", env)
	}

	uc := userCredFromBinary(context.Background(), bin)
	if uc == nil || string(uc.EncryptedEnv) != `{"TOKEN":"t"}` || uc.CredentialType == nil || *uc.CredentialType != "pat" {
		t.Fatalf("adapter credential not the labeled one: %+v", uc)
	}
}

func orcaScriptTool(t *testing.T, labels []store.SecureCLICredentialLabel, withDefault bool) (*ExecTool, *labeledStubStore, context.Context) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX-only")
	}
	binPath := filepath.Join(t.TempDir(), "orca")
	script := "#!/bin/sh\nprintf 'ARGS:%s\\n' \"$*\"\nprintf 'CODE:%s\\n' \"$(printf %s \"$ORCA_PAIRING_CODE\" | tr a-z A-Z)\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	preset := GetPreset("orca")
	if preset == nil {
		t.Fatal("orca preset missing")
	}
	deny, err := json.Marshal(preset.DenyArgs)
	if err != nil {
		t.Fatal(err)
	}
	row := &store.SecureCLIBinary{
		BaseModel:      store.BaseModel{ID: uuid.New()},
		BinaryName:     "orca",
		BinaryPath:     &binPath,
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       deny,
		DenyVerbose:    json.RawMessage("[]"),
	}
	if withDefault {
		row.SetEffectiveCredential([]byte(`{"ORCA_PAIRING_CODE":"default-code"}`), nil, nil, "user", "u1")
	}
	stub := newStubSecureCLIStore()
	stub.byName["orca"] = row
	ls := &labeledStubStore{
		stubSecureCLIStore: stub,
		labels:             labels,
		resolved: map[string]*store.SecureCLIResolvedCredential{
			"build-vm":   {Label: "build-vm", Source: "user", SubjectID: "u1", Env: []byte(`{"ORCA_PAIRING_CODE":"build-code"}`)},
			"design-mac": {Label: "design-mac", Source: "agent", Env: []byte(`{"ORCA_PAIRING_CODE":"design-code"}`)},
		},
	}
	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(ls)
	ctx := store.WithUserID(store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New()), "u1")
	return tool, ls, ctx
}

var twoServers = []store.SecureCLICredentialLabel{
	{Label: "build-vm", Description: "CI build VM"},
	{Label: "design-mac", Description: "Design Mac mini"},
}

func TestExecServerSelectionUsesLabeledEnvAndStripsSelector(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers, true)
	res := tool.Execute(ctx, map[string]any{"command": "orca --server build-vm status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "ARGS:status --json") || strings.Contains(res.ForLLM, "--server") {
		t.Fatalf("selector not stripped: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:BUILD-CODE") {
		t.Fatalf("labeled env not injected: %s", res.ForLLM)
	}
}

func TestExecServerSelectionEqualsFormAgentLabel(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers, true)
	res := tool.Execute(ctx, map[string]any{"command": "orca --server=design-mac status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:DESIGN-CODE") || strings.Contains(res.ForLLM, "DEFAULT-CODE") {
		t.Fatalf("wrong env: %s", res.ForLLM)
	}
}

func TestExecServerSelectionNoSelectorKeepsDefaultWithoutQueries(t *testing.T) {
	tool, ls, ctx := orcaScriptTool(t, twoServers, true)
	res := tool.Execute(ctx, map[string]any{"command": "orca status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:DEFAULT-CODE") {
		t.Fatalf("default env expected: %s", res.ForLLM)
	}
	if ls.listCalls != 0 {
		t.Fatalf("expected no label queries, got %d", ls.listCalls)
	}
}

func TestExecServerSelectionDefaultAlias(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers, true)
	res := tool.Execute(ctx, map[string]any{"command": "orca --server default status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:DEFAULT-CODE") || !strings.Contains(res.ForLLM, "ARGS:status --json") {
		t.Fatalf("default alias mishandled: %s", res.ForLLM)
	}
}

func TestExecServerSelectionAmbiguousWithoutDefault(t *testing.T) {
	tool, ls, ctx := orcaScriptTool(t, twoServers, false)
	res := tool.Execute(ctx, map[string]any{"command": "orca status --json"})
	if !res.IsError {
		t.Fatalf("expected ambiguity error, got: %s", res.ForLLM)
	}
	for _, want := range []string{"Multiple servers configured for orca", "build-vm — CI build VM", "design-mac — Design Mac mini"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Fatalf("missing %q in: %s", want, res.ForLLM)
		}
	}
	if strings.Contains(res.ForLLM, "ARGS:") || len(ls.resolveCall) != 0 {
		t.Fatalf("command must not execute: %s", res.ForLLM)
	}
}

func TestExecServerSelectionSingleLabelWithoutDefault(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers[:1], false)
	res := tool.Execute(ctx, map[string]any{"command": "orca status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:BUILD-CODE") {
		t.Fatalf("single label expected: %s", res.ForLLM)
	}
}

func TestExecServerSelectionUnknownLabelTruncatedAndSafe(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, twoServers, true)
	long := strings.Repeat("a", 500)
	res := tool.Execute(ctx, map[string]any{"command": "orca --server " + long + " status"})
	if !res.IsError {
		t.Fatalf("expected unknown server error")
	}
	if strings.Contains(res.ForLLM, strings.Repeat("a", 65)) {
		t.Fatalf("label echoed untruncated: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Unknown server") || !strings.Contains(res.ForLLM, "build-vm") {
		t.Fatalf("unexpected message: %s", res.ForLLM)
	}
	for _, secret := range []string{"build-code", "design-code", "default-code"} {
		if strings.Contains(res.ForLLM, secret) {
			t.Fatalf("error leaks env value %q", secret)
		}
	}
}

func TestExecServerSelectionDoesNotBypassDeny(t *testing.T) {
	for _, command := range []string{
		"orca --server build-vm --pairing-code orca://pair?x status",
		"orca --server build-vm --json status",
		"orca --server build-vm status --environment other",
		"orca --server build-vm serve",
		"orca --server --pairing-code status",
		"orca --server=build-vm --pairing-code=x status",
	} {
		tool, _, ctx := orcaScriptTool(t, twoServers, true)
		res := tool.Execute(ctx, map[string]any{"command": command})
		if !res.IsError || strings.Contains(res.ForLLM, "ARGS:") {
			t.Fatalf("%q must be blocked, got: %s", command, res.ForLLM)
		}
	}
}

func TestExecServerSelectionPassthroughWhenNoLabels(t *testing.T) {
	tool, _, ctx := orcaScriptTool(t, nil, true)
	res := tool.Execute(ctx, map[string]any{"command": "orca --server x status"})
	if !res.IsError || !strings.Contains(res.ForLLM, "blocked by security policy") {
		t.Fatalf("raw selector must reach deny check when no labels exist: %s", res.ForLLM)
	}
}

func TestExecServerSelectionListErrorWithSelector(t *testing.T) {
	tool, ls, ctx := orcaScriptTool(t, twoServers, false)
	ls.listErr = errors.New("db down")
	res := tool.Execute(ctx, map[string]any{"command": "orca --server build-vm status"})
	if !res.IsError || strings.Contains(res.ForLLM, "db down") {
		t.Fatalf("expected generic lookup error: %s", res.ForLLM)
	}
}

func TestExecServerSelectionBinaryLevelEnvCountsAsDefault(t *testing.T) {
	tool, ls, ctx := orcaScriptTool(t, twoServers, false)
	stub := ls.stubSecureCLIStore
	stub.byName["orca"].EncryptedEnv = []byte(`{"ORCA_PAIRING_CODE":"binary-code"}`)
	res := tool.Execute(ctx, map[string]any{"command": "orca status --json"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "CODE:BINARY-CODE") {
		t.Fatalf("binary env expected: %s", res.ForLLM)
	}
	if ls.listCalls != 0 {
		t.Fatalf("expected no label queries, got %d", ls.listCalls)
	}
}
