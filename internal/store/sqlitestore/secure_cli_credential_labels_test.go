//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func newLabelFixture(t *testing.T) (*SQLiteSecureCLIStore, *sql.DB, context.Context, *store.SecureCLIBinary, uuid.UUID) {
	t.Helper()
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-labels-"+uuid.NewString()[:8])
	ctx := store.WithTenantID(context.Background(), tid)
	agentID := seedAgent(t, db, tid, "labeler-"+uuid.NewString()[:8])
	bin := &store.SecureCLIBinary{
		BinaryName:   "orca",
		Description:  "orca",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "tester",
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s, db, ctx, bin, agentID
}

func TestSQLite_CredentialLabelColumnsExist(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)
	for _, table := range []string{"secure_cli_user_credentials", "secure_cli_agent_credentials"} {
		for _, col := range []string{"label", "description"} {
			if !columnExists(t, db, table, col) {
				t.Fatalf("%s.%s missing", table, col)
			}
		}
	}
}

func TestSQLite_UserCredentialLabels_CoexistAndDuplicateRejected(t *testing.T) {
	s, db, ctx, bin, _ := newLabelFixture(t)
	if err := s.SetUserCredentials(ctx, bin.ID, "u-1", []byte(`{"K":"default"}`)); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"design-mac", "build-vm"} {
		if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: l, Description: "d " + l, Env: []byte(`{"K":"` + l + `"}`)}); err != nil {
			t.Fatalf("set %s: %v", l, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM secure_cli_user_credentials WHERE binary_id = ? AND user_id = 'u-1'`, bin.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("expected 3 rows, got %d err=%v", n, err)
	}
	_, err := db.Exec(`INSERT INTO secure_cli_user_credentials (id, binary_id, user_id, encrypted_env, tenant_id, label) SELECT ?, binary_id, user_id, encrypted_env, tenant_id, label FROM secure_cli_user_credentials WHERE label = 'build-vm'`, uuid.NewString())
	if err == nil {
		t.Fatal("expected duplicate label insert to fail")
	}
}

func TestSQLite_LabeledRowsDoNotAffectDefaultOperations(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	if err := s.SetUserCredentials(ctx, bin.ID, "u-1", []byte(`{"K":"default-user"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"mac-user"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentCredentials(ctx, bin.ID, agentID, []byte(`{"K":"default-agent"}`), "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"mac-agent"}`)}, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := s.LookupByBinary(ctx, "orca", &agentID, "u-1")
	if err != nil || got == nil {
		t.Fatalf("LookupByBinary: %v %v", got, err)
	}
	if got.CredentialSource != "user" || string(got.CredentialEnv) != `{"K":"default-user"}` {
		t.Fatalf("default lookup picked wrong row: source=%q env=%q", got.CredentialSource, got.CredentialEnv)
	}

	uc, err := s.GetUserCredentials(ctx, bin.ID, "u-1")
	if err != nil || uc == nil || uc.Label != "" || string(uc.EncryptedEnv) != `{"K":"default-user"}` {
		t.Fatalf("GetUserCredentials wrong: %+v err=%v", uc, err)
	}
	ac, err := s.GetAgentCredentials(ctx, bin.ID, agentID)
	if err != nil || ac == nil || ac.Label != "" || string(ac.EncryptedEnv) != `{"K":"default-agent"}` {
		t.Fatalf("GetAgentCredentials wrong: %+v err=%v", ac, err)
	}

	if err := s.DeleteUserCredentials(ctx, bin.ID, "u-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAgentCredentials(ctx, bin.ID, agentID); err != nil {
		t.Fatal(err)
	}
	if uc, _ := s.GetUserCredentialByLabel(ctx, bin.ID, "u-1", "mac"); uc == nil {
		t.Fatal("deleting default user credential removed labeled row")
	}
	if ac, _ := s.GetAgentCredentialByLabel(ctx, bin.ID, agentID, "mac"); ac == nil {
		t.Fatal("deleting default agent credential removed labeled row")
	}
	if uc, _ := s.GetUserCredentials(ctx, bin.ID, "u-1"); uc != nil {
		t.Fatal("default user credential should be gone")
	}
}

func TestSQLite_LabeledOnlyLookupReturnsNoDefaultCredential(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"mac"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupByBinary(ctx, "orca", &agentID, "u-1")
	if err != nil || got == nil {
		t.Fatalf("LookupByBinary: %v %v", got, err)
	}
	if got.CredentialSource != "" || len(got.UserEnv) != 0 {
		t.Fatalf("labeled-only user must not resolve as default: source=%q env=%q", got.CredentialSource, got.UserEnv)
	}
}

func TestSQLite_LabeledUpsertAndDescriptionPreservation(t *testing.T) {
	s, _, ctx, bin, _ := newLabelFixture(t)
	w := store.SecureCLICredentialWrite{Label: "Mac", Description: "Design\nMac", Env: []byte(`{"K":"1"}`)}
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", w); err != nil {
		t.Fatal(err)
	}
	w.Env = []byte(`{"K":"2"}`)
	w.Description = "second"
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", w); err != nil {
		t.Fatal(err)
	}
	uc, err := s.GetUserCredentialByLabel(ctx, bin.ID, "u-1", "mac")
	if err != nil || uc == nil {
		t.Fatalf("get: %v %v", uc, err)
	}
	if uc.Label != "mac" || uc.Description != "second" || string(uc.EncryptedEnv) != `{"K":"2"}` {
		t.Fatalf("upsert wrong: %+v env=%q", uc, uc.EncryptedEnv)
	}

	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "", Description: "keepme", Env: []byte(`{"K":"d"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserCredentials(ctx, bin.ID, "u-1", []byte(`{"K":"d2"}`)); err != nil {
		t.Fatal(err)
	}
	def, _ := s.GetUserCredentials(ctx, bin.ID, "u-1")
	if def == nil || def.Description != "keepme" || string(def.EncryptedEnv) != `{"K":"d2"}` {
		t.Fatalf("legacy write must not clobber description: %+v", def)
	}
}

func TestSQLite_InvalidLabelRejected(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "bad label", Env: []byte(`{}`)}); err == nil {
		t.Fatal("expected invalid label error")
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "--x", Env: []byte(`{}`)}, "a"); err == nil {
		t.Fatal("expected invalid label error")
	}
}

func TestSQLite_DeleteByLabelLeavesOthers(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	for _, l := range []string{"a", "b"} {
		if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: l, Env: []byte(`{"K":"` + l + `"}`)}, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteAgentCredentialByLabel(ctx, bin.ID, agentID, "a"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetAgentCredentialByLabel(ctx, bin.ID, agentID, "a"); c != nil {
		t.Fatal("label a should be deleted")
	}
	if c, _ := s.GetAgentCredentialByLabel(ctx, bin.ID, agentID, "b"); c == nil {
		t.Fatal("label b should remain")
	}
	list, err := s.ListAgentCredentials(ctx, bin.ID)
	if err != nil || len(list) != 1 || list[0].Label != "b" {
		t.Fatalf("list wrong: %+v err=%v", list, err)
	}
}

func TestSQLite_ResolveLabeledCredential_UserShadowsAgent(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	pat := "pat"
	host := "example.com"
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"agent"}`), CredentialType: &pat, HostScope: &host}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "vm", Env: []byte(`{"K":"vm-agent"}`)}, "admin"); err != nil {
		t.Fatal(err)
	}

	res, err := s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "MAC")
	if err != nil || res == nil {
		t.Fatalf("resolve: %v %v", res, err)
	}
	if res.Source != "agent" || res.SubjectID != "" || string(res.Env) != `{"K":"agent"}` || res.CredentialType == nil || *res.CredentialType != "pat" {
		t.Fatalf("expected agent row, got %+v env=%q", res, res.Env)
	}

	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"user"}`)}); err != nil {
		t.Fatal(err)
	}
	res, err = s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "mac")
	if err != nil || res == nil || res.Source != "user" || res.SubjectID != "u-1" || string(res.Env) != `{"K":"user"}` {
		t.Fatalf("expected user row to shadow agent: %+v err=%v", res, err)
	}

	res, err = s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-2", "mac")
	if err != nil || res == nil || res.Source != "agent" {
		t.Fatalf("other user should fall back to agent: %+v err=%v", res, err)
	}

	res, err = s.ResolveLabeledCredential(ctx, bin.ID, &agentID, "u-1", "missing")
	if err != nil || res != nil {
		t.Fatalf("unknown label should be nil,nil: %+v err=%v", res, err)
	}
	res, err = s.ResolveLabeledCredential(ctx, bin.ID, nil, "", "mac")
	if err != nil || res != nil {
		t.Fatalf("no subject should be nil,nil: %+v err=%v", res, err)
	}
}

func TestSQLite_ResolveLabeledCredential_CrossTenantInvisible(t *testing.T) {
	s, db, ctx, bin, agentID := newLabelFixture(t)
	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"user"}`)}); err != nil {
		t.Fatal(err)
	}
	otherCtx := store.WithTenantID(context.Background(), seedTenant(t, db, "t-labels-other"))
	res, err := s.ResolveLabeledCredential(otherCtx, bin.ID, &agentID, "u-1", "mac")
	if err != nil || res != nil {
		t.Fatalf("cross-tenant row visible: %+v err=%v", res, err)
	}
	labels, err := s.ListCredentialLabels(otherCtx, store.SecureCLICredentialLabelQuery{UserID: "u-1"})
	if err != nil || len(labels) != 0 {
		t.Fatalf("cross-tenant labels visible: %+v err=%v", labels, err)
	}
	if labels, err := s.ListCredentialLabels(context.Background(), store.SecureCLICredentialLabelQuery{UserID: "u-1"}); err != nil || labels != nil {
		t.Fatalf("missing tenant should return nil: %+v err=%v", labels, err)
	}
}

func TestSQLite_ListCredentialLabels_MergesAndFiltersByBinary(t *testing.T) {
	s, _, ctx, bin, agentID := newLabelFixture(t)
	other := &store.SecureCLIBinary{BinaryName: "kubectl", Description: "k", IsGlobal: true, Enabled: true, CreatedBy: "t", EncryptedEnv: []byte(`{}`)}
	if err := s.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SetUserCredentials(ctx, bin.ID, "u-1", []byte(`{"K":"d"}`)))
	must(s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Description: "user mac", Env: []byte(`{"K":"1"}`)}))
	must(s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "mac", Description: "agent mac", Env: []byte(`{"K":"2"}`)}, "a"))
	must(s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "vm", Description: "agent vm", Env: []byte(`{"K":"3"}`)}, "a"))
	must(s.SetUserCredentialLabeled(ctx, other.ID, "u-1", store.SecureCLICredentialWrite{Label: "k1", Env: []byte(`{"K":"4"}`)}))

	all, err := s.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{AgentID: &agentID, UserID: "u-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 merged labels, got %d: %+v", len(all), all)
	}

	scoped, err := s.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{BinaryID: &bin.ID, AgentID: &agentID, UserID: "u-1"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.SecureCLICredentialLabel{}
	for _, l := range scoped {
		if l.BinaryID != bin.ID {
			t.Fatalf("binary filter leaked %+v", l)
		}
		got[l.Label] = l
	}
	if len(scoped) != 3 || got[""].Source != "user" || got["mac"].Source != "user" || got["mac"].Description != "user mac" || got["vm"].Source != "agent" {
		t.Fatalf("unexpected scoped labels: %+v", scoped)
	}

	agentOnly, err := s.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{AgentID: &agentID, BinaryID: &bin.ID})
	if err != nil || len(agentOnly) != 2 {
		t.Fatalf("agent-only labels: %+v err=%v", agentOnly, err)
	}
	if none, err := s.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{}); err != nil || none != nil {
		t.Fatalf("empty query should return nil: %+v err=%v", none, err)
	}
}

const legacyCredentialTablesDDL = `
DROP TABLE secure_cli_user_credentials;
DROP TABLE secure_cli_agent_credentials;
CREATE TABLE secure_cli_user_credentials (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_id       TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id         VARCHAR(255) NOT NULL,
    encrypted_env   BLOB NOT NULL,
    metadata        TEXT NOT NULL DEFAULT '{}',
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    credential_type TEXT,
    host_scope      TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, user_id, tenant_id)
);
CREATE TABLE secure_cli_agent_credentials (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_id       TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    encrypted_env   BLOB NOT NULL,
    metadata        TEXT NOT NULL DEFAULT '{}',
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    credential_type TEXT,
    host_scope      TEXT,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, agent_id, tenant_id),
    FOREIGN KEY (binary_id, tenant_id) REFERENCES secure_cli_binaries(id, tenant_id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id, tenant_id) REFERENCES agents(id, tenant_id) ON DELETE CASCADE
);`

func TestSQLite_CredentialLabelsMigration_PreservesLegacyRows(t *testing.T) {
	s, db, ctx, bin, agentID := newLabelFixture(t)
	tid := store.TenantIDFromContext(ctx)

	if _, err := db.Exec(legacyCredentialTablesDDL); err != nil {
		t.Fatalf("recreate legacy tables: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO secure_cli_user_credentials (id, binary_id, user_id, encrypted_env, tenant_id, credential_type, host_scope) VALUES (?, ?, 'u-1', x'7b7d', ?, 'pat', 'github.com')`, uuid.NewString(), bin.ID, tid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO secure_cli_agent_credentials (id, binary_id, agent_id, encrypted_env, tenant_id, created_by) VALUES (?, ?, ?, x'7b7d', ?, 'admin')`, uuid.NewString(), bin.ID, agentID, tid); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(migrations[60]); err != nil {
		tx.Rollback()
		t.Fatalf("apply migration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	uc, err := s.GetUserCredentials(ctx, bin.ID, "u-1")
	if err != nil || uc == nil || uc.Label != "" || uc.Description != "" || uc.CredentialType == nil || *uc.CredentialType != "pat" {
		t.Fatalf("legacy user row not preserved: %+v err=%v", uc, err)
	}
	ac, err := s.GetAgentCredentials(ctx, bin.ID, agentID)
	if err != nil || ac == nil || ac.Label != "" || ac.CreatedBy != "admin" {
		t.Fatalf("legacy agent row not preserved: %+v err=%v", ac, err)
	}

	if err := s.SetUserCredentialLabeled(ctx, bin.ID, "u-1", store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"1"}`)}); err != nil {
		t.Fatalf("labeled write after migration: %v", err)
	}
	if err := s.SetAgentCredentialLabeled(ctx, bin.ID, agentID, store.SecureCLICredentialWrite{Label: "mac", Env: []byte(`{"K":"1"}`)}, "admin"); err != nil {
		t.Fatalf("labeled agent write after migration: %v", err)
	}
}
