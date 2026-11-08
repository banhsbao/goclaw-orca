//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const orcaTestCode = "orca://host.example:6768#secret-code"

func newOrcaFixture(t *testing.T) (labelFixture, *pg.PGOrcaServerStore) {
	t.Helper()
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binID := seedGateBinary(t, db, tenantID, "orca", true)
	t.Cleanup(func() {
		db.Exec("DELETE FROM secure_cli_agent_credentials WHERE binary_id = $1", binID)
		db.Exec("DELETE FROM orca_servers WHERE tenant_id = $1", tenantID)
	})
	f := labelFixture{db: db, cli: pg.NewPGSecureCLIStore(db, testEncryptionKey), tenantID: tenantID, agentID: agentID, binID: binID, binName: "orca"}
	return f, pg.NewPGOrcaServerStore(db, testEncryptionKey)
}

func createOrca(t *testing.T, s *pg.PGOrcaServerStore, f labelFixture, label string) *store.OrcaServer {
	t.Helper()
	srv := &store.OrcaServer{Label: label, Description: "desc " + label, PairingCode: orcaTestCode, CreatedBy: "tester"}
	mustNoErr(t, s.Create(f.ctx(), srv), "Create "+label)
	return srv
}

func TestOrcaServers_CRUDRoundTrip(t *testing.T) {
	f, s := newOrcaFixture(t)
	ctx := f.ctx()
	srv := createOrca(t, s, f, "build-vm")

	got, err := s.Get(ctx, srv.ID)
	mustNoErr(t, err, "Get")
	if got.Label != "build-vm" || got.Description != "desc build-vm" || !got.HasPairingCode || got.PairingCode != "" || got.LastStatus != store.OrcaServerStatusUnknown {
		t.Fatalf("Get: %+v", got)
	}
	secret, err := s.GetWithSecret(ctx, srv.ID)
	mustNoErr(t, err, "GetWithSecret")
	if secret.PairingCode != orcaTestCode {
		t.Fatalf("GetWithSecret code: %q", secret.PairingCode)
	}

	dup := &store.OrcaServer{Label: "build-vm", PairingCode: orcaTestCode}
	if err := s.Create(ctx, dup); !errors.Is(err, store.ErrOrcaServerLabelTaken) {
		t.Fatalf("duplicate label: %v", err)
	}

	desc, newCode := "changed", "orca://other:1#new"
	mustNoErr(t, s.Update(ctx, srv.ID, &desc, &newCode), "Update")
	secret, err = s.GetWithSecret(ctx, srv.ID)
	mustNoErr(t, err, "GetWithSecret after update")
	if secret.Description != "changed" || secret.PairingCode != newCode {
		t.Fatalf("after update: %+v", secret)
	}

	list, err := s.List(ctx)
	mustNoErr(t, err, "List")
	if len(list) != 1 {
		t.Fatalf("List: got %d", len(list))
	}

	mustNoErr(t, s.Delete(ctx, srv.ID), "Delete")
	if _, err := s.Get(ctx, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}

func TestOrcaServers_TenantIsolation(t *testing.T) {
	f, s := newOrcaFixture(t)
	tenantB, _ := seedTenantAgent(t, f.db)
	t.Cleanup(func() { f.db.Exec("DELETE FROM orca_servers WHERE tenant_id = $1", tenantB) })
	srv := createOrca(t, s, f, "isolated")
	ctxB := tenantCtx(tenantB)

	if _, err := s.Get(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("Get from other tenant: %v", err)
	}
	if _, err := s.GetWithSecret(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("GetWithSecret from other tenant: %v", err)
	}
	if err := s.Delete(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("Delete from other tenant: %v", err)
	}
	list, err := s.List(ctxB)
	mustNoErr(t, err, "List tenant B")
	if len(list) != 0 {
		t.Fatalf("tenant B sees %d servers", len(list))
	}
	if _, err := s.Get(f.ctx(), srv.ID); err != nil {
		t.Fatalf("owner lost server: %v", err)
	}
}

func TestOrcaServers_PairingCodeEncryptedAtRest(t *testing.T) {
	f, s := newOrcaFixture(t)
	srv := createOrca(t, s, f, "enc")
	var raw []byte
	mustNoErr(t, f.db.QueryRow(`SELECT pairing_code FROM orca_servers WHERE id = $1`, srv.ID).Scan(&raw), "raw select")
	if len(raw) == 0 || string(raw) == orcaTestCode {
		t.Fatalf("pairing code stored in plaintext: %q", raw)
	}
}

func TestOrcaServers_LabelSourceOnlyForOrcaBinary(t *testing.T) {
	f, s := newOrcaFixture(t)
	createOrca(t, s, f, "build-vm")
	otherBin := seedGateBinary(t, f.db, f.tenantID, "other-"+uuid.New().String()[:8], true)

	labels, err := f.cli.ListCredentialLabels(f.ctx(), store.SecureCLICredentialLabelQuery{BinaryID: &f.binID, AgentID: &f.agentID, UserID: "u1"})
	mustNoErr(t, err, "list orca")
	l, ok := labelMap(labels)["build-vm"]
	if !ok || l.Source != store.SecureCLISourceOrcaServer {
		t.Fatalf("orca labels: %+v", labels)
	}

	labels, err = f.cli.ListCredentialLabels(f.ctx(), store.SecureCLICredentialLabelQuery{BinaryID: &otherBin, AgentID: &f.agentID, UserID: "u1"})
	mustNoErr(t, err, "list other")
	if len(labels) != 0 {
		t.Fatalf("other binary got labels: %+v", labels)
	}
}

func TestOrcaServers_LabelPrecedence(t *testing.T) {
	f, s := newOrcaFixture(t)
	ctx := f.ctx()
	user := "u-prec-" + uuid.New().String()[:8]
	createOrca(t, s, f, "build-vm")
	setAgentLabel(t, f, "build-vm", "agent", `{"ORCA_PAIRING_CODE":"agent"}`)
	setUserLabel(t, f, user, "build-vm", "user", `{"ORCA_PAIRING_CODE":"user"}`)

	got, err := f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "build-vm")
	mustNoErr(t, err, "resolve user")
	if got == nil || got.Source != "user" {
		t.Fatalf("want user, got %+v", got)
	}

	mustNoErr(t, f.cli.DeleteUserCredentialByLabel(ctx, f.binID, user, "build-vm"), "delete user cred")
	got, err = f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "build-vm")
	mustNoErr(t, err, "resolve agent")
	if got == nil || got.Source != "agent" {
		t.Fatalf("want agent, got %+v", got)
	}

	mustNoErr(t, f.cli.DeleteAgentCredentialByLabel(ctx, f.binID, f.agentID, "build-vm"), "delete agent cred")
	got, err = f.cli.ResolveLabeledCredential(ctx, f.binID, &f.agentID, user, "build-vm")
	mustNoErr(t, err, "resolve orca_server")
	want, _ := store.OrcaServerCredentialEnv(orcaTestCode)
	if got == nil || got.Source != store.SecureCLISourceOrcaServer || string(got.Env) != string(want) {
		t.Fatalf("want orca_server env %s, got %+v", want, got)
	}
}

func TestOrcaServers_CrossTenantNotResolved(t *testing.T) {
	f, s := newOrcaFixture(t)
	fB, sB := newOrcaFixture(t)
	createOrca(t, sB, fB, "only-b")
	_ = s

	labels, err := f.cli.ListCredentialLabels(f.ctx(), store.SecureCLICredentialLabelQuery{BinaryID: &f.binID, AgentID: &f.agentID, UserID: "u1"})
	mustNoErr(t, err, "list tenant A")
	if _, ok := labelMap(labels)["only-b"]; ok {
		t.Fatalf("tenant A lists tenant B server: %+v", labels)
	}
	got, err := f.cli.ResolveLabeledCredential(f.ctx(), f.binID, &f.agentID, "u1", "only-b")
	mustNoErr(t, err, "resolve tenant A")
	if got != nil {
		t.Fatalf("tenant A resolved tenant B server: %+v", got)
	}
	got, err = f.cli.ResolveLabeledCredential(f.ctx(), fB.binID, &f.agentID, "u1", "only-b")
	mustNoErr(t, err, "resolve foreign binary")
	if got != nil {
		t.Fatalf("resolved with foreign binary: %+v", got)
	}
}

func TestOrcaServers_HealthAndDueForCheck(t *testing.T) {
	f, s := newOrcaFixture(t)
	ctx := f.ctx()
	stale := createOrca(t, s, f, "stale")
	fresh := createOrca(t, s, f, "fresh")
	now := time.Now().UTC()

	mustNoErr(t, s.UpdateHealth(ctx, stale.ID, store.OrcaServerHealth{Status: store.OrcaServerStatusHealthy, RuntimeState: "ready", CheckedAt: now.Add(-10 * time.Minute), Healthy: true}), "health stale")
	mustNoErr(t, s.UpdateHealth(ctx, fresh.ID, store.OrcaServerHealth{Status: store.OrcaServerStatusError, Error: "boom", CheckedAt: now, Healthy: false}), "health fresh")

	got, err := s.Get(ctx, stale.ID)
	mustNoErr(t, err, "Get stale")
	if got.LastStatus != store.OrcaServerStatusHealthy || got.LastRuntimeState != "ready" || got.LastCheckedAt == nil || got.LastHealthyAt == nil {
		t.Fatalf("stale health: %+v", got)
	}
	got, err = s.Get(ctx, fresh.ID)
	mustNoErr(t, err, "Get fresh")
	if got.LastStatus != store.OrcaServerStatusError || got.LastError != "boom" || got.LastHealthyAt != nil {
		t.Fatalf("fresh health: %+v", got)
	}

	due, err := s.ListDueForCheck(ctx, now.Add(-5*time.Minute), 100)
	mustNoErr(t, err, "ListDueForCheck")
	ids := map[uuid.UUID]bool{}
	for _, d := range due {
		ids[d.ID] = true
		if d.PairingCode != orcaTestCode {
			t.Fatalf("due row missing decrypted code: %+v", d)
		}
	}
	if !ids[stale.ID] || ids[fresh.ID] {
		t.Fatalf("due ids wrong: stale=%v fresh=%v", ids[stale.ID], ids[fresh.ID])
	}
}
