//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func addOrcaServer(t *testing.T, s *SQLiteSecureCLIStore, ctx context.Context, label, code string) {
	t.Helper()
	os := NewSQLiteOrcaServerStore(s.db, orcaTestKey)
	if err := os.Create(ctx, &store.OrcaServer{Label: label, Description: "d " + label, PairingCode: code}); err != nil {
		t.Fatalf("create orca server %s: %v", label, err)
	}
}

func labelSources(t *testing.T, s *SQLiteSecureCLIStore, ctx context.Context, q store.SecureCLICredentialLabelQuery) map[string]string {
	t.Helper()
	labels, err := s.ListCredentialLabels(ctx, q)
	if err != nil {
		t.Fatalf("ListCredentialLabels: %v", err)
	}
	out := map[string]string{}
	for _, l := range labels {
		out[l.Label] = l.Source
	}
	return out
}

func TestSQLite_OrcaServerLabels_FallbackWhenNoCredentials(t *testing.T) {
	s, _, ctx, bin, agentID := orcaLabelFixture(t)
	addOrcaServer(t, s, ctx, "build-vm", "orca://h#build")

	b, err := s.LookupByBinary(ctx, "orca", &agentID, "u-1")
	if err != nil || b == nil {
		t.Fatalf("LookupByBinary without credentials: b=%v err=%v", b, err)
	}
	if b.CredentialSource != "" {
		t.Fatalf("expected no default credential source, got %q", b.CredentialSource)
	}

	q := store.SecureCLICredentialLabelQuery{BinaryID: &bin.ID, AgentID: &agentID, UserID: "u-1"}
	if got := labelSources(t, s, ctx, q); len(got) != 1 || got["build-vm"] != "orca_server" {
		t.Fatalf("unexpected labels: %v", got)
	}

	res, err := s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "build-vm")
	if err != nil || res == nil {
		t.Fatalf("resolve: res=%v err=%v", res, err)
	}
	if res.Source != "orca_server" || res.SubjectID == "" || string(res.Env) != `{"ORCA_PAIRING_CODE":"orca://h#build"}` {
		t.Fatalf("unexpected resolved credential: %+v env=%s", res, res.Env)
	}

	miss, err := s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "nope")
	if err != nil || miss != nil {
		t.Fatalf("unknown label: res=%v err=%v", miss, err)
	}
}

func TestSQLite_OrcaServerLabels_CredentialsWinOnLabelClash(t *testing.T) {
	s, _, ctx, bin, agentID := orcaLabelFixture(t)
	addOrcaServer(t, s, ctx, "shared", "orca://h#server")
	addOrcaServer(t, s, ctx, "only-server", "orca://h#only")
	addOrcaServer(t, s, ctx, "agent-clash", "orca://h#clash")
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "shared", Env: []byte(`{"ORCA_PAIRING_CODE":"user"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "shared", Env: []byte(`{"ORCA_PAIRING_CODE":"agent"}`)}, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "agent-clash", Env: []byte(`{"ORCA_PAIRING_CODE":"agent2"}`)}, "t"); err != nil {
		t.Fatal(err)
	}

	q := store.SecureCLICredentialLabelQuery{BinaryID: &bin.ID, AgentID: &agentID, UserID: "u-1"}
	got := labelSources(t, s, ctx, q)
	if got["shared"] != "user" || got["agent-clash"] != "agent" || got["only-server"] != "orca_server" || len(got) != 3 {
		t.Fatalf("unexpected precedence: %v", got)
	}

	res, _ := s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "shared")
	if res == nil || res.Source != "user" {
		t.Fatalf("user credential must win: %+v", res)
	}
	res, _ = s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "", "shared")
	if res == nil || res.Source != "agent" {
		t.Fatalf("agent credential must beat server: %+v", res)
	}
	res, _ = s.ResolveLabeledCredential(ctx, bin.ID, nil, "", "shared")
	if res == nil || res.Source != "orca_server" {
		t.Fatalf("server is the fallback without user/agent: %+v", res)
	}
}

func TestSQLite_OrcaServerLabels_NotExposedToOtherBinariesOrTenants(t *testing.T) {
	s, db, ctx, bin, agentID := orcaLabelFixture(t)
	addOrcaServer(t, s, ctx, "build-vm", "orca://h#build")

	other := &store.SecureCLIBinary{BinaryName: "gh", IsGlobal: true, Enabled: true, CreatedBy: "t", EncryptedEnv: []byte(`{}`)}
	if err := s.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	q := store.SecureCLICredentialLabelQuery{BinaryID: &other.ID, AgentID: &agentID}
	if got := labelSources(t, s, ctx, q); len(got) != 0 {
		t.Fatalf("non-orca binary must not see servers: %v", got)
	}
	if res, err := s.ResolveLabeledCredential(ctx, other.ID, &agentID, "", "build-vm"); err != nil || res != nil {
		t.Fatalf("non-orca resolve: res=%v err=%v", res, err)
	}

	otherTenant := store.WithTenantID(context.Background(), seedTenant(t, db, "t-other-"+uuid.NewString()[:8]))
	q = store.SecureCLICredentialLabelQuery{BinaryID: &bin.ID, AgentID: &agentID}
	if got := labelSources(t, s, otherTenant, q); len(got) != 0 {
		t.Fatalf("other tenant must not see servers: %v", got)
	}
	if res, err := s.ResolveLabeledCredential(otherTenant, bin.ID, &agentID, "", "build-vm"); err != nil || res != nil {
		t.Fatalf("cross-tenant resolve: res=%v err=%v", res, err)
	}

	if got := labelSources(t, s, ctx, store.SecureCLICredentialLabelQuery{BinaryID: &bin.ID}); len(got) != 0 {
		t.Fatalf("no user or agent subject yields no labels: %v", got)
	}
}

func orcaLabelFixture(t *testing.T) (*SQLiteSecureCLIStore, *sql.DB, context.Context, *store.SecureCLIBinary, uuid.UUID) {
	t.Helper()
	_, db, ctx, bin, agentID := newLabelFixture(t)
	return NewSQLiteSecureCLIStore(db, orcaTestKey), db, ctx, bin, agentID
}
