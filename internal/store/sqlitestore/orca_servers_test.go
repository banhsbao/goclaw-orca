//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const orcaTestKey = "0123456789abcdef0123456789abcdef"

func newOrcaTestStore(t *testing.T, key string) (*SQLiteOrcaServerStore, context.Context, context.Context) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "orca.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	ctxs := make([]context.Context, 2)
	for i := range ctxs {
		tid := store.GenNewID()
		if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
			VALUES (?, 't', ?, 'active', '{}', datetime('now'), datetime('now'))`, tid.String(), "t-"+tid.String()); err != nil {
			t.Fatal(err)
		}
		ctxs[i] = store.WithTenantID(context.Background(), tid)
	}
	return NewSQLiteOrcaServerStore(db, key), ctxs[0], ctxs[1]
}

func TestOrcaServerCRUD(t *testing.T) {
	s, ctx, _ := newOrcaTestStore(t, orcaTestKey)
	srv := &store.OrcaServer{Label: "Prod", Description: "main", PairingCode: "orca://h#secret", CreatedBy: "u1"}
	if err := s.Create(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &store.OrcaServer{Label: "prod", PairingCode: "orca://h#x"}); !errors.Is(err, store.ErrOrcaServerLabelTaken) {
		t.Fatalf("want label taken, got %v", err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].Label != "prod" || list[0].PairingCode != "" || !list[0].HasPairingCode {
		t.Fatalf("list: %+v %v", list, err)
	}
	got, err := s.GetWithSecret(ctx, srv.ID)
	if err != nil || got.PairingCode != "orca://h#secret" {
		t.Fatalf("secret: %+v %v", got, err)
	}
	plain, _ := s.Get(ctx, srv.ID)
	if plain.PairingCode != "" {
		t.Fatal("Get must not return secret")
	}
	desc, code := "new\ndesc", "orca://h#rotated"
	if err := s.Update(ctx, srv.ID, &desc, &code); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWithSecret(ctx, srv.ID)
	if got.Description != "new desc" || got.PairingCode != code {
		t.Fatalf("update: %+v", got)
	}
	bad := "https://x"
	if err := s.Update(ctx, srv.ID, nil, &bad); err == nil {
		t.Fatal("invalid code accepted")
	}
	if err := s.Delete(ctx, srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if err := s.Delete(ctx, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestOrcaServerTenantIsolation(t *testing.T) {
	s, ctxA, ctxB := newOrcaTestStore(t, orcaTestKey)
	srv := &store.OrcaServer{Label: "shared", PairingCode: "orca://h#a"}
	if err := s.Create(ctxA, srv); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctxB, &store.OrcaServer{Label: "shared", PairingCode: "orca://h#b"}); err != nil {
		t.Fatalf("same label in other tenant: %v", err)
	}
	if _, err := s.Get(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if _, err := s.GetWithSecret(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant secret get: %v", err)
	}
	d := "x"
	if err := s.Update(ctxB, srv.ID, &d, nil); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant update: %v", err)
	}
	if err := s.Delete(ctxB, srv.ID); !errors.Is(err, store.ErrOrcaServerNotFound) {
		t.Fatalf("cross-tenant delete: %v", err)
	}
	if l, _ := s.List(context.Background()); len(l) != 0 {
		t.Fatal("nil tenant must list nothing")
	}
}

func TestOrcaServerPairingCodeEncryptedAtRest(t *testing.T) {
	s, ctx, _ := newOrcaTestStore(t, orcaTestKey)
	srv := &store.OrcaServer{Label: "enc", PairingCode: "orca://h#plaintext-marker"}
	if err := s.Create(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRow(`SELECT pairing_code FROM orca_servers WHERE id = ?`, srv.ID.String()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || strings.Contains(string(raw), "plaintext-marker") {
		t.Fatalf("pairing code stored in plaintext: %q", raw)
	}
	other := NewSQLiteOrcaServerStore(s.db, "ffffffffffffffffffffffffffffffff")
	if _, err := other.GetWithSecret(ctx, srv.ID); err == nil {
		t.Fatal("wrong key must fail closed")
	}
}

func TestOrcaServerLimitPerTenant(t *testing.T) {
	s, ctx, _ := newOrcaTestStore(t, orcaTestKey)
	for i := 0; i < store.OrcaServerMaxPerTenant; i++ {
		label := "s" + string(rune('a'+i))
		if err := s.Create(ctx, &store.OrcaServer{Label: label, PairingCode: "orca://h#c"}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if err := s.Create(ctx, &store.OrcaServer{Label: "over", PairingCode: "orca://h#c"}); !errors.Is(err, store.ErrOrcaServerLimit) {
		t.Fatalf("want limit, got %v", err)
	}
}

func TestOrcaServerHealthAndDueForCheck(t *testing.T) {
	s, ctx, _ := newOrcaTestStore(t, orcaTestKey)
	a := &store.OrcaServer{Label: "a", PairingCode: "orca://h#a"}
	b := &store.OrcaServer{Label: "b", PairingCode: "orca://h#b"}
	for _, x := range []*store.OrcaServer{a, b} {
		if err := s.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if err := s.UpdateHealth(ctx, a.ID, store.OrcaServerHealth{Status: "healthy", RuntimeState: "running", CheckedAt: now, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, a.ID)
	if got.LastStatus != "healthy" || got.LastCheckedAt == nil || got.LastHealthyAt == nil {
		t.Fatalf("health not stored: %+v", got)
	}
	if err := s.UpdateHealth(ctx, a.ID, store.OrcaServerHealth{Status: "error", Error: "boom", CheckedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, a.ID)
	if got.LastHealthyAt == nil || got.LastError != "boom" {
		t.Fatalf("last_healthy_at must persist: %+v", got)
	}
	due, err := s.ListDueForCheck(context.Background(), now.Add(-time.Minute), 10)
	if err != nil || len(due) != 1 || due[0].ID != b.ID || due[0].PairingCode != "orca://h#b" {
		t.Fatalf("due: %+v %v", due, err)
	}
	due, _ = s.ListDueForCheck(context.Background(), now.Add(time.Hour), 10)
	if len(due) != 2 || due[0].ID != b.ID {
		t.Fatalf("due ordering: %+v", due)
	}
}

func TestOrcaServerUpgradeFromSchema61(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE orca_servers`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 61`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("upgrade from 61: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatalf("version = %d, err = %v, want %d", version, err, SchemaVersion)
	}
	tid := store.GenNewID()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		VALUES (?, 't', ?, 'active', '{}', datetime('now'), datetime('now'))`, tid.String(), "t-"+tid.String()); err != nil {
		t.Fatal(err)
	}
	s := NewSQLiteOrcaServerStore(db, orcaTestKey)
	ctx := store.WithTenantID(context.Background(), tid)
	srv := &store.OrcaServer{Label: "up", PairingCode: "orca://h#c"}
	if err := s.Create(ctx, srv); err != nil {
		t.Fatalf("create after upgrade: %v", err)
	}
	got, err := s.GetWithSecret(ctx, srv.ID)
	if err != nil || got.PairingCode != "orca://h#c" {
		t.Fatalf("get after upgrade: %+v, %v", got, err)
	}
}
