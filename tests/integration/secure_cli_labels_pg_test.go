//go:build integration

package integration

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

type labelFixture struct {
	db       *sql.DB
	cli      *pg.PGSecureCLIStore
	tenantID uuid.UUID
	agentID  uuid.UUID
	binID    uuid.UUID
	binName  string
}

func newLabelFixture(t *testing.T) labelFixture {
	t.Helper()
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binName := "labels-bin-" + uuid.New().String()[:8]
	binID := seedGateBinary(t, db, tenantID, binName, true)
	t.Cleanup(func() {
		db.Exec("DELETE FROM secure_cli_agent_credentials WHERE binary_id = $1", binID)
	})
	return labelFixture{
		db:       db,
		cli:      pg.NewPGSecureCLIStore(db, testEncryptionKey),
		tenantID: tenantID,
		agentID:  agentID,
		binID:    binID,
		binName:  binName,
	}
}

func (f labelFixture) ctx() context.Context {
	return tenantCtx(f.tenantID)
}

func mustNoErr(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func setUserLabel(t *testing.T, f labelFixture, user, label, desc, env string) {
	t.Helper()
	mustNoErr(t, f.cli.SetUserCredentialLabeled(f.ctx(), f.binID, user, store.SecureCLICredentialWrite{
		Label: label, Description: desc, Env: []byte(env),
	}), "SetUserCredentialLabeled "+label)
}

func setAgentLabel(t *testing.T, f labelFixture, label, desc, env string) {
	t.Helper()
	mustNoErr(t, f.cli.SetAgentCredentialLabeled(f.ctx(), f.binID, f.agentID, store.SecureCLICredentialWrite{
		Label: label, Description: desc, Env: []byte(env),
	}, "tester"), "SetAgentCredentialLabeled "+label)
}

func labelMap(labels []store.SecureCLICredentialLabel) map[string]store.SecureCLICredentialLabel {
	m := make(map[string]store.SecureCLICredentialLabel, len(labels))
	for _, l := range labels {
		m[l.Label] = l
	}
	return m
}

func TestSecureCLILabels_DefaultAndLabeledRowsCoexist(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-coexist-" + uuid.New().String()[:8]

	mustNoErr(t, f.cli.SetUserCredentials(ctx, f.binID, user, []byte(`{"TOKEN":"default"}`)), "legacy set")
	setUserLabel(t, f, user, "work", "Work box", `{"TOKEN":"work"}`)
	setUserLabel(t, f, user, "home", "Home box", `{"TOKEN":"home"}`)

	def, err := f.cli.GetUserCredentials(ctx, f.binID, user)
	mustNoErr(t, err, "GetUserCredentials")
	if def == nil || string(def.EncryptedEnv) != `{"TOKEN":"default"}` {
		t.Fatalf("default row: got %+v, want env default", def)
	}

	work, err := f.cli.GetUserCredentialByLabel(ctx, f.binID, user, "work")
	mustNoErr(t, err, "GetUserCredentialByLabel work")
	if work == nil || string(work.EncryptedEnv) != `{"TOKEN":"work"}` || work.Description != "Work box" || work.Label != "work" {
		t.Fatalf("work row: got %+v", work)
	}

	all, err := f.cli.ListUserCredentials(ctx, f.binID)
	mustNoErr(t, err, "ListUserCredentials")
	if len(all) != 3 {
		t.Fatalf("ListUserCredentials: got %d rows, want 3 (default+work+home)", len(all))
	}

	b, err := f.cli.LookupByBinary(ctx, f.binName, nil, user)
	mustNoErr(t, err, "LookupByBinary")
	if b == nil {
		t.Fatalf("LookupByBinary returned nil")
	}
	if string(b.UserEnv) != `{"TOKEN":"default"}` {
		t.Fatalf("LookupByBinary UserEnv: got %q, want default row env", b.UserEnv)
	}
}

func TestSecureCLILabels_UserRowShadowsAgentRowForSameLabel(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-shadow-" + uuid.New().String()[:8]

	setAgentLabel(t, f, "pat", "agent pat", `{"TOKEN":"agent"}`)
	setUserLabel(t, f, user, "pat", "user pat", `{"TOKEN":"user"}`)

	got, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "pat")
	mustNoErr(t, err, "resolve shadowed")
	if got == nil || got.Source != "user" || got.SubjectID != user || string(got.Env) != `{"TOKEN":"user"}` {
		t.Fatalf("user with pat: got %+v, want user source + user env", got)
	}

	other, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, "u-no-row-"+user, "pat")
	mustNoErr(t, err, "resolve other user")
	if other == nil || other.Source != "agent" || other.SubjectID != "" || string(other.Env) != `{"TOKEN":"agent"}` {
		t.Fatalf("user without pat row: got %+v, want agent source + agent env", other)
	}

	anon, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, "", "pat")
	mustNoErr(t, err, "resolve no user")
	if anon == nil || anon.Source != "agent" {
		t.Fatalf("no user id: got %+v, want agent source", anon)
	}

	upper, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "  PAT ")
	mustNoErr(t, err, "resolve normalized label")
	if upper == nil || upper.Source != "user" {
		t.Fatalf("normalized label: got %+v, want user source", upper)
	}

	missing, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "missing")
	mustNoErr(t, err, "resolve missing")
	if missing != nil {
		t.Fatalf("missing label: got %+v, want nil", missing)
	}
}

func TestSecureCLILabels_ListMergesUserAndAgentRowsPerBinary(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-list-" + uuid.New().String()[:8]
	otherBinID := seedGateBinary(t, f.db, f.tenantID, "labels-other-"+uuid.New().String()[:8], true)

	mustNoErr(t, f.cli.SetUserCredentials(ctx, f.binID, user, []byte(`{"TOKEN":"default"}`)), "legacy set")
	setUserLabel(t, f, user, "work", "user-work", `{"TOKEN":"uw"}`)
	setAgentLabel(t, f, "work", "agent-work", `{"TOKEN":"aw"}`)
	setAgentLabel(t, f, "ci", "agent-ci", `{"TOKEN":"ci"}`)
	mustNoErr(t, f.cli.SetUserCredentialLabeled(ctx, otherBinID, user, store.SecureCLICredentialWrite{
		Label: "other", Env: []byte(`{}`),
	}), "other binary label")

	both, err := f.cli.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{
		BinaryID: &f.binID, AgentID: &f.agentID, UserID: user,
	})
	mustNoErr(t, err, "ListCredentialLabels both")
	if len(both) != 3 {
		t.Fatalf("merged labels: got %d (%+v), want 3 (\"\", ci, work)", len(both), both)
	}
	if both[0].Label != "" || both[1].Label != "ci" || both[2].Label != "work" {
		t.Fatalf("merged labels order: got %+v, want \"\", ci, work", both)
	}
	m := labelMap(both)
	if m["work"].Source != "user" || m["work"].Description != "user-work" {
		t.Fatalf("work must resolve to user row, got %+v", m["work"])
	}
	if m["ci"].Source != "agent" || m["ci"].Description != "agent-ci" {
		t.Fatalf("ci must come from agent row, got %+v", m["ci"])
	}
	if m[""].Source != "user" {
		t.Fatalf("default row source: got %q, want user", m[""].Source)
	}
	for _, l := range both {
		if l.BinaryID != f.binID {
			t.Fatalf("binary filter leaked binary %s into result", l.BinaryID)
		}
	}

	userOnly, err := f.cli.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{
		BinaryID: &f.binID, UserID: user,
	})
	mustNoErr(t, err, "ListCredentialLabels user only")
	if len(userOnly) != 2 || userOnly[0].Label != "" || userOnly[1].Label != "work" {
		t.Fatalf("user-only labels: got %+v, want \"\", work", userOnly)
	}

	agentOnly, err := f.cli.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{
		BinaryID: &f.binID, AgentID: &f.agentID,
	})
	mustNoErr(t, err, "ListCredentialLabels agent only")
	if len(agentOnly) != 2 || agentOnly[0].Label != "ci" || agentOnly[1].Label != "work" || agentOnly[1].Source != "agent" {
		t.Fatalf("agent-only labels: got %+v, want ci, work(agent)", agentOnly)
	}
}

func TestSecureCLILabels_LegacySetGetDeleteLeaveLabeledRowsAlone(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-legacy-" + uuid.New().String()[:8]

	mustNoErr(t, f.cli.SetUserCredentialLabeled(ctx, f.binID, user, store.SecureCLICredentialWrite{
		Label: "", Description: "Main account", Env: []byte(`{"TOKEN":"main-1"}`),
	}), "label-empty write sets default description")
	setUserLabel(t, f, user, "work", "Work", `{"TOKEN":"work"}`)

	mustNoErr(t, f.cli.SetUserCredentials(ctx, f.binID, user, []byte(`{"TOKEN":"main-2"}`)), "legacy re-set")
	def, err := f.cli.GetUserCredentials(ctx, f.binID, user)
	mustNoErr(t, err, "GetUserCredentials after legacy set")
	if def == nil || string(def.EncryptedEnv) != `{"TOKEN":"main-2"}` {
		t.Fatalf("legacy set did not update default env: got %+v", def)
	}
	if def.Description != "Main account" {
		t.Fatalf("legacy set clobbered default description: got %q", def.Description)
	}
	work, err := f.cli.GetUserCredentialByLabel(ctx, f.binID, user, "work")
	mustNoErr(t, err, "GetUserCredentialByLabel after legacy set")
	if work == nil || string(work.EncryptedEnv) != `{"TOKEN":"work"}` || work.Description != "Work" {
		t.Fatalf("legacy set touched labeled row: got %+v", work)
	}

	mustNoErr(t, f.cli.DeleteUserCredentials(ctx, f.binID, user), "legacy delete")
	def, err = f.cli.GetUserCredentials(ctx, f.binID, user)
	mustNoErr(t, err, "GetUserCredentials after legacy delete")
	if def != nil {
		t.Fatalf("legacy delete left default row: %+v", def)
	}
	work, err = f.cli.GetUserCredentialByLabel(ctx, f.binID, user, "work")
	mustNoErr(t, err, "work after legacy delete")
	if work == nil || string(work.EncryptedEnv) != `{"TOKEN":"work"}` {
		t.Fatalf("legacy delete removed labeled row: got %+v", work)
	}

	labeledOnly := "u-labeled-only-" + uuid.New().String()[:8]
	setUserLabel(t, f, labeledOnly, "work", "Work", `{"TOKEN":"lo"}`)
	legacyView, err := f.cli.GetUserCredentials(ctx, f.binID, labeledOnly)
	mustNoErr(t, err, "legacy get for labeled-only user")
	if legacyView != nil {
		t.Fatalf("legacy Get must not return labeled rows, got %+v", legacyView)
	}

	mustNoErr(t, f.cli.SetAgentCredentialLabeled(ctx, f.binID, f.agentID, store.SecureCLICredentialWrite{
		Label: "", Description: "Agent main", Env: []byte(`{"TOKEN":"a-main-1"}`),
	}, "tester"), "agent label-empty write")
	setAgentLabel(t, f, "pat", "Agent pat", `{"TOKEN":"a-pat"}`)

	mustNoErr(t, f.cli.SetAgentCredentials(ctx, f.binID, f.agentID, []byte(`{"TOKEN":"a-main-2"}`), "tester"), "agent legacy set")
	ag, err := f.cli.GetAgentCredentials(ctx, f.binID, f.agentID)
	mustNoErr(t, err, "GetAgentCredentials after legacy set")
	if ag == nil || string(ag.EncryptedEnv) != `{"TOKEN":"a-main-2"}` || ag.Description != "Agent main" {
		t.Fatalf("agent legacy set: got %+v, want updated env and kept description", ag)
	}
	agPat, err := f.cli.GetAgentCredentialByLabel(ctx, f.binID, f.agentID, "pat")
	mustNoErr(t, err, "agent pat after legacy set")
	if agPat == nil || string(agPat.EncryptedEnv) != `{"TOKEN":"a-pat"}` {
		t.Fatalf("agent legacy set touched labeled row: got %+v", agPat)
	}

	mustNoErr(t, f.cli.DeleteAgentCredentials(ctx, f.binID, f.agentID), "agent legacy delete")
	ag, err = f.cli.GetAgentCredentials(ctx, f.binID, f.agentID)
	mustNoErr(t, err, "agent get after legacy delete")
	if ag != nil {
		t.Fatalf("agent legacy delete left default row: %+v", ag)
	}
	agPat, err = f.cli.GetAgentCredentialByLabel(ctx, f.binID, f.agentID, "pat")
	mustNoErr(t, err, "agent pat after legacy delete")
	if agPat == nil {
		t.Fatalf("agent legacy delete removed labeled row")
	}
}

func TestSecureCLILabels_DeleteByLabelKeepsOtherRows(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-delbylabel-" + uuid.New().String()[:8]

	mustNoErr(t, f.cli.SetUserCredentials(ctx, f.binID, user, []byte(`{"TOKEN":"default"}`)), "legacy set")
	setUserLabel(t, f, user, "work", "Work", `{"TOKEN":"work"}`)
	setUserLabel(t, f, user, "home", "Home", `{"TOKEN":"home"}`)
	setAgentLabel(t, f, "work", "agent work", `{"TOKEN":"aw"}`)
	setAgentLabel(t, f, "ci", "agent ci", `{"TOKEN":"ci"}`)

	mustNoErr(t, f.cli.DeleteUserCredentialByLabel(ctx, f.binID, user, "work"), "delete user work")
	mustNoErr(t, f.cli.DeleteAgentCredentialByLabel(ctx, f.binID, f.agentID, "ci"), "delete agent ci")

	labels, err := f.cli.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{
		BinaryID: &f.binID, AgentID: &f.agentID, UserID: user,
	})
	mustNoErr(t, err, "list after delete by label")
	m := labelMap(labels)
	if _, ok := m["work"]; !ok || m["work"].Source != "agent" {
		t.Fatalf("after deleting user work, agent work must surface: got %+v", m["work"])
	}
	if _, ok := m["ci"]; ok {
		t.Fatalf("deleted agent ci still listed")
	}
	if _, ok := m["home"]; !ok {
		t.Fatalf("home removed by unrelated delete")
	}
	if _, ok := m[""]; !ok {
		t.Fatalf("default row removed by label delete")
	}

	def, err := f.cli.GetUserCredentials(ctx, f.binID, user)
	mustNoErr(t, err, "default after label delete")
	if def == nil {
		t.Fatalf("default user row removed by label delete")
	}
}

func TestSecureCLILabels_CrossTenantRowsInvisible(t *testing.T) {
	fA := newLabelFixture(t)
	fB := newLabelFixture(t)
	ctxA := fA.ctx()
	ctxB := fB.ctx()
	user := "u-xt-" + uuid.New().String()[:8]

	setUserLabel(t, fA, user, "work", "A work", `{"TOKEN":"a"}`)
	setAgentLabel(t, fA, "ci", "A ci", `{"TOKEN":"a-ci"}`)

	labelsA, err := fA.cli.ListCredentialLabels(ctxA, store.SecureCLICredentialLabelQuery{
		BinaryID: &fA.binID, AgentID: &fA.agentID, UserID: user,
	})
	mustNoErr(t, err, "tenant A list")
	if len(labelsA) != 2 {
		t.Fatalf("sanity: tenant A should see 2 labels, got %+v", labelsA)
	}

	labelsB, err := fB.cli.ListCredentialLabels(ctxB, store.SecureCLICredentialLabelQuery{
		BinaryID: &fA.binID, AgentID: &fA.agentID, UserID: user,
	})
	mustNoErr(t, err, "tenant B list")
	if len(labelsB) != 0 {
		t.Fatalf("tenant B LEAK: listed tenant A labels %+v", labelsB)
	}

	if got, err := fB.cli.ResolveLabeledCredential(ctxB, fA.binID, &fA.agentID, user, "work"); err != nil || got != nil {
		t.Fatalf("tenant B resolved user label of tenant A: got=%+v err=%v", got, err)
	}
	if got, err := fB.cli.ResolveLabeledCredential(ctxB, fA.binID, &fA.agentID, "", "ci"); err != nil || got != nil {
		t.Fatalf("tenant B resolved agent label of tenant A: got=%+v err=%v", got, err)
	}
	if got, err := fB.cli.GetUserCredentialByLabel(ctxB, fA.binID, user, "work"); err != nil || got != nil {
		t.Fatalf("tenant B read user label of tenant A: got=%+v err=%v", got, err)
	}
	if got, err := fB.cli.GetAgentCredentialByLabel(ctxB, fA.binID, fA.agentID, "ci"); err != nil || got != nil {
		t.Fatalf("tenant B read agent label of tenant A: got=%+v err=%v", got, err)
	}
	if got, err := fB.cli.LookupByBinary(ctxB, fA.binName, &fA.agentID, user); err != nil || got != nil {
		t.Fatalf("tenant B LookupByBinary of tenant A binary: got=%+v err=%v", got, err)
	}

	writeErr := fB.cli.SetAgentCredentialLabeled(ctxB, fA.binID, fA.agentID, store.SecureCLICredentialWrite{
		Label: "evil", Env: []byte(`{"TOKEN":"x"}`),
	}, "attacker")
	if writeErr == nil {
		t.Fatalf("tenant B wrote agent label onto tenant A binary/agent")
	}
	if got, _ := fA.cli.GetAgentCredentialByLabel(ctxA, fA.binID, fA.agentID, "evil"); got != nil {
		t.Fatalf("cross-tenant agent write persisted for tenant A")
	}
}

func TestSecureCLILabels_LookupByBinaryIgnoresLabeledRows(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-lookup-" + uuid.New().String()[:8]
	other := "u-lookup-other-" + uuid.New().String()[:8]

	setUserLabel(t, f, user, "work", "Work", `{"TOKEN":"uw"}`)
	setAgentLabel(t, f, "pat", "Agent pat", `{"TOKEN":"ap"}`)

	b, err := f.cli.LookupByBinary(ctx, f.binName, &f.agentID, user)
	mustNoErr(t, err, "lookup labeled-only")
	if b == nil {
		t.Fatalf("lookup returned nil")
	}
	if b.UserEnv != nil || b.UserCredentialType != nil || b.UserHostScope != nil {
		t.Fatalf("labeled user row leaked into default user env: UserEnv=%q", b.UserEnv)
	}
	if b.CredentialSource != "" || len(b.CredentialEnv) != 0 {
		t.Fatalf("labeled agent row leaked into default selection: source=%q", b.CredentialSource)
	}

	mustNoErr(t, f.cli.SetUserCredentials(ctx, f.binID, user, []byte(`{"TOKEN":"default-user"}`)), "default user set")
	mustNoErr(t, f.cli.SetAgentCredentials(ctx, f.binID, f.agentID, []byte(`{"TOKEN":"default-agent"}`), "tester"), "default agent set")

	b, err = f.cli.LookupByBinary(ctx, f.binName, &f.agentID, user)
	mustNoErr(t, err, "lookup with defaults")
	if string(b.UserEnv) != `{"TOKEN":"default-user"}` || b.CredentialSource != "user" || string(b.CredentialEnv) != `{"TOKEN":"default-user"}` {
		t.Fatalf("default user selection changed by labeled rows: source=%q env=%q", b.CredentialSource, b.CredentialEnv)
	}

	b, err = f.cli.LookupByBinary(ctx, f.binName, &f.agentID, other)
	mustNoErr(t, err, "lookup other user")
	if b.UserEnv != nil {
		t.Fatalf("other user inherited default-user env: %q", b.UserEnv)
	}
	if b.CredentialSource != "agent" || string(b.CredentialEnv) != `{"TOKEN":"default-agent"}` {
		t.Fatalf("default agent selection changed by labeled rows: source=%q env=%q", b.CredentialSource, b.CredentialEnv)
	}
}

func TestSecureCLILabels_AgentLabelDoesNotInheritDefaultUserEnv(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-stale-" + uuid.New().String()[:8]

	mustNoErr(t, f.cli.SetUserCredentialsTyped(ctx, f.binID, user,
		[]byte(`{"GH_TOKEN":"user-default-secret"}`), strPtr("pat"), strPtr("github.com")), "typed default user set")
	setAgentLabel(t, f, "pat", "Agent pat", `{"GH_TOKEN":"agent-pat-secret"}`)

	base, err := f.cli.LookupByBinary(ctx, f.binName, &f.agentID, user)
	mustNoErr(t, err, "base lookup")
	if base == nil || string(base.UserEnv) != `{"GH_TOKEN":"user-default-secret"}` ||
		base.UserCredentialType == nil || base.UserHostScope == nil {
		t.Fatalf("precondition failed: base lookup must carry stale default-user fields, got %+v", base)
	}

	resolved, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "pat")
	mustNoErr(t, err, "resolve agent pat")
	if resolved == nil || resolved.Source != "agent" {
		t.Fatalf("resolve must fall through to agent row, got %+v", resolved)
	}

	selected := *base
	selected.ApplyLabeledCredential(resolved)

	if selected.UserEnv != nil {
		t.Fatalf("stale UserEnv survived labeled selection: %q", selected.UserEnv)
	}
	if selected.UserCredentialType != nil || selected.UserHostScope != nil {
		t.Fatalf("stale user type/host scope survived: type=%v host=%v", selected.UserCredentialType, selected.UserHostScope)
	}
	if selected.CredentialSubjectID != "" {
		t.Fatalf("stale subject survived: %q", selected.CredentialSubjectID)
	}
	if selected.CredentialSource != "agent" || string(selected.CredentialEnv) != `{"GH_TOKEN":"agent-pat-secret"}` {
		t.Fatalf("labeled selection wrong: source=%q env=%q", selected.CredentialSource, selected.CredentialEnv)
	}
	if selected.CredentialType != nil || selected.CredentialHostScope != nil {
		t.Fatalf("agent label inherited typed metadata from default user: type=%v host=%v",
			selected.CredentialType, selected.CredentialHostScope)
	}
	if strings.Contains(string(selected.CredentialEnv), "user-default-secret") {
		t.Fatalf("default user secret present in selected credential env")
	}
}

func TestSecureCLILabels_LegacyAndLabeledWritesShareTenant(t *testing.T) {
	f := newLabelFixture(t)
	noTenant := context.Background()
	ctxT := f.ctx()
	userNil := "u-tenant-nil-" + uuid.New().String()[:8]
	userSet := "u-tenant-set-" + uuid.New().String()[:8]

	mustNoErr(t, f.cli.SetUserCredentials(noTenant, f.binID, userNil, []byte(`{"TOKEN":"legacy"}`)), "legacy write without tenant")
	mustNoErr(t, f.cli.SetUserCredentialLabeled(noTenant, f.binID, userNil, store.SecureCLICredentialWrite{
		Label: "work", Env: []byte(`{"TOKEN":"labeled"}`),
	}), "labeled write without tenant")

	rowsNil := tenantsOfUserRows(t, f.db, f.binID, userNil)
	if len(rowsNil) != 2 {
		t.Fatalf("expected 2 user rows for nil-tenant writes, got %d", len(rowsNil))
	}
	for _, tid := range rowsNil {
		if tid != store.MasterTenantID {
			t.Fatalf("nil-tenant write landed in %s, want MasterTenantID %s", tid, store.MasterTenantID)
		}
	}

	got, err := f.cli.GetUserCredentialByLabel(noTenant, f.binID, userNil, "work")
	mustNoErr(t, err, "read labeled back under nil tenant")
	if got == nil || string(got.EncryptedEnv) != `{"TOKEN":"labeled"}` {
		t.Fatalf("nil-tenant labeled write not readable under same fallback: %+v", got)
	}

	mustNoErr(t, f.cli.SetUserCredentials(ctxT, f.binID, userSet, []byte(`{"TOKEN":"legacy"}`)), "legacy write with tenant")
	mustNoErr(t, f.cli.SetUserCredentialLabeled(ctxT, f.binID, userSet, store.SecureCLICredentialWrite{
		Label: "work", Env: []byte(`{"TOKEN":"labeled"}`),
	}), "labeled write with tenant")
	for _, tid := range tenantsOfUserRows(t, f.db, f.binID, userSet) {
		if tid != f.tenantID {
			t.Fatalf("tenant-scoped write landed in %s, want %s", tid, f.tenantID)
		}
	}

	legacyAgentErr := f.cli.SetAgentCredentials(noTenant, f.binID, f.agentID, []byte(`{}`), "tester")
	labeledAgentErr := f.cli.SetAgentCredentialLabeled(noTenant, f.binID, f.agentID, store.SecureCLICredentialWrite{
		Label: "pat", Env: []byte(`{}`),
	}, "tester")
	if legacyAgentErr == nil || labeledAgentErr == nil {
		t.Fatalf("agent writes without tenant must fail on both paths: legacy=%v labeled=%v", legacyAgentErr, labeledAgentErr)
	}
}

func TestSecureCLILabels_WriteValidationAndDescriptionSanitized(t *testing.T) {
	f := newLabelFixture(t)
	ctx := f.ctx()
	user := "u-validate-" + uuid.New().String()[:8]

	for _, bad := range []string{"Bad Label!", "-leading", strings.Repeat("a", 33)} {
		err := f.cli.SetUserCredentialLabeled(ctx, f.binID, user, store.SecureCLICredentialWrite{
			Label: bad, Env: []byte(`{}`),
		})
		if err == nil {
			t.Fatalf("label %q accepted, want validation error", bad)
		}
	}
	var n int
	mustNoErr(t, f.db.QueryRow(`SELECT COUNT(*) FROM secure_cli_user_credentials WHERE binary_id = $1 AND user_id = $2`,
		f.binID, user).Scan(&n), "count invalid writes")
	if n != 0 {
		t.Fatalf("invalid labels persisted %d rows", n)
	}

	longDesc := "line1\nline2" + strings.Repeat("é", 300)
	setUserLabel(t, f, user, "desc", longDesc, `{}`)
	got, err := f.cli.GetUserCredentialByLabel(ctx, f.binID, user, "desc")
	mustNoErr(t, err, "read sanitized description")
	want := "line1 line2" + strings.Repeat("é", 189)
	if got == nil || got.Description != want {
		t.Fatalf("description sanitize: got len %d, want %q-prefixed 200-rune value", len([]rune(got.Description)), "line1 line2")
	}
}

func tenantsOfUserRows(t *testing.T, db *sql.DB, binID uuid.UUID, user string) []uuid.UUID {
	t.Helper()
	rows, err := db.Query(`SELECT tenant_id FROM secure_cli_user_credentials WHERE binary_id = $1 AND user_id = $2`, binID, user)
	mustNoErr(t, err, "query tenants")
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		mustNoErr(t, rows.Scan(&id), "scan tenant")
		out = append(out, id)
	}
	mustNoErr(t, rows.Err(), "rows tenant")
	return out
}

func TestSecureCLILabels_UniqueConstraintsAndSchema(t *testing.T) {
	f := newLabelFixture(t)
	db := f.db

	for _, col := range []struct{ table, column string }{
		{"secure_cli_user_credentials", "label"},
		{"secure_cli_user_credentials", "description"},
		{"secure_cli_agent_credentials", "label"},
		{"secure_cli_agent_credentials", "description"},
	} {
		if !pgColumnExists(t, db, col.table, col.column) {
			t.Fatalf("%s.%s missing", col.table, col.column)
		}
	}

	user := "u-uniq-" + uuid.New().String()[:8]
	insertUser := func(userID, label string) error {
		_, err := db.Exec(`INSERT INTO secure_cli_user_credentials (binary_id, user_id, encrypted_env, tenant_id, label)
			VALUES ($1, $2, $3, $4, $5)`, f.binID, userID, []byte(`{}`), f.tenantID, label)
		return err
	}
	insertAgent := func(label string) error {
		_, err := db.Exec(`INSERT INTO secure_cli_agent_credentials (binary_id, agent_id, encrypted_env, tenant_id, label)
			VALUES ($1, $2, $3, $4, $5)`, f.binID, f.agentID, []byte(`{}`), f.tenantID, label)
		return err
	}

	mustNoErr(t, insertUser(user, ""), "insert default user row")
	mustNoErr(t, insertUser(user, "work"), "insert distinct user label")
	if err := insertUser(user, "work"); err == nil || !strings.Contains(err.Error(), "uq_scuc_binary_user_tenant_label") {
		t.Fatalf("duplicate user label must violate uq_scuc_binary_user_tenant_label, got %v", err)
	}
	if err := insertUser(user, ""); err == nil || !strings.Contains(err.Error(), "uq_scuc_binary_user_tenant_label") {
		t.Fatalf("duplicate default user row must be rejected, got %v", err)
	}
	mustNoErr(t, insertUser("u-uniq-other-"+user, "work"), "same label different subject")

	mustNoErr(t, insertAgent(""), "insert default agent row")
	mustNoErr(t, insertAgent("pat"), "insert distinct agent label")
	if err := insertAgent("pat"); err == nil || !strings.Contains(err.Error(), "uq_scac_binary_agent_tenant_label") {
		t.Fatalf("duplicate agent label must violate uq_scac_binary_agent_tenant_label, got %v", err)
	}

	assertUniqueConstraints(t, db, "secure_cli_user_credentials", []string{"uq_scuc_binary_user_tenant_label"})
	assertUniqueConstraints(t, db, "secure_cli_agent_credentials", []string{"uq_scac_binary_agent_tenant_label"})
}

func assertUniqueConstraints(t *testing.T, db *sql.DB, table string, want []string) {
	t.Helper()
	rows, err := db.Query(`SELECT conname FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'u' ORDER BY conname`, table)
	mustNoErr(t, err, "query unique constraints "+table)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		mustNoErr(t, rows.Scan(&name), "scan constraint")
		got = append(got, name)
	}
	mustNoErr(t, rows.Err(), "rows constraints")
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s unique constraints: got %v, want %v (legacy UNIQUE must be dropped)", table, got, want)
	}
}

func TestSecureCLILabels_MigrationAppliesFromScratchKeepsLegacyRowsAsDefault(t *testing.T) {
	admin := testDB(t)
	dbName := "goclaw_mig_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	if _, err := admin.Exec(`CREATE DATABASE ` + dbName); err != nil {
		t.Fatalf("create scratch db: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec(`DROP DATABASE IF EXISTS ` + dbName + ` WITH (FORCE)`)
	})

	dsn := labelsDSN()
	u, err := url.Parse(dsn)
	mustNoErr(t, err, "parse dsn")
	u.Path = "/" + dbName
	scratchDSN := u.String()

	m, err := migrate.New("file://../../migrations", scratchDSN)
	mustNoErr(t, err, "migrate.New scratch")
	t.Cleanup(func() { m.Close() })

	mustNoErr(t, m.Migrate(99), "migrate to 99 from scratch")

	scratch, err := sql.Open("pgx", scratchDSN)
	mustNoErr(t, err, "open scratch")
	t.Cleanup(func() { scratch.Close() })

	tenantID := uuid.New()
	agentID := uuid.New()
	binID := uuid.New()
	mustNoErr(t, execAll(scratch,
		[]string{
			`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active')`,
			`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
			 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')`,
			`INSERT INTO secure_cli_binaries (id, tenant_id, binary_name, encrypted_env, description, enabled, is_global)
			 VALUES ($1, $2, $3, $4, 'migration test', true, true)`,
			`INSERT INTO secure_cli_user_credentials (binary_id, user_id, encrypted_env, tenant_id) VALUES ($1, $2, $3, $4)`,
			`INSERT INTO secure_cli_agent_credentials (binary_id, agent_id, encrypted_env, tenant_id) VALUES ($1, $2, $3, $4)`,
		},
		[][]any{
			{tenantID, "mig-tenant", "mig" + tenantID.String()[:8]},
			{agentID, tenantID, "mig-agent-" + agentID.String()[:8]},
			{binID, tenantID, "mig-bin-" + binID.String()[:8], []byte(`{}`)},
			{binID, "u-legacy-mig", []byte(`legacy-user-env`), tenantID},
			{binID, agentID, []byte(`legacy-agent-env`), tenantID},
		},
	), "seed pre-000100 rows")

	mustNoErr(t, m.Up(), "migrate up to 100")

	var userLabel, userDesc string
	mustNoErr(t, scratch.QueryRow(`SELECT label, description FROM secure_cli_user_credentials WHERE binary_id = $1 AND user_id = 'u-legacy-mig'`,
		binID).Scan(&userLabel, &userDesc), "legacy user row after migration")
	if userLabel != "" || userDesc != "" {
		t.Fatalf("legacy user row not default after migration: label=%q desc=%q", userLabel, userDesc)
	}
	var agentLabel, agentDesc string
	mustNoErr(t, scratch.QueryRow(`SELECT label, description FROM secure_cli_agent_credentials WHERE binary_id = $1 AND agent_id = $2`,
		binID, agentID).Scan(&agentLabel, &agentDesc), "legacy agent row after migration")
	if agentLabel != "" || agentDesc != "" {
		t.Fatalf("legacy agent row not default after migration: label=%q desc=%q", agentLabel, agentDesc)
	}

	assertUniqueConstraints(t, scratch, "secure_cli_user_credentials", []string{"uq_scuc_binary_user_tenant_label"})
	assertUniqueConstraints(t, scratch, "secure_cli_agent_credentials", []string{"uq_scac_binary_agent_tenant_label"})

	mustNoErr(t, insertLabeledUser(scratch, binID, "u-legacy-mig", "work", tenantID), "labeled row alongside legacy default")
	if err := insertLabeledUser(scratch, binID, "u-legacy-mig", "work", tenantID); err == nil {
		t.Fatalf("duplicate migrated label accepted")
	}
	mustNoErr(t, insertLabeledUser(scratch, binID, "u-other-mig", "work", tenantID), "same label different subject after migration")
}

func labelsDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

func insertLabeledUser(db *sql.DB, binID uuid.UUID, userID, label string, tenantID uuid.UUID) error {
	_, err := db.Exec(`INSERT INTO secure_cli_user_credentials (binary_id, user_id, encrypted_env, tenant_id, label)
		VALUES ($1, $2, $3, $4, $5)`, binID, userID, []byte(`{}`), tenantID, label)
	return err
}

func execAll(db *sql.DB, stmts []string, args [][]any) error {
	for i, s := range stmts {
		if _, err := db.Exec(s, args[i]...); err != nil {
			return err
		}
	}
	return nil
}
