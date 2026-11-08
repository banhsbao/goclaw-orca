package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/orcaserver"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	orcaTestToken = "orca-test-token"
	orcaTestCode  = "orca://pair?very-secret-payload"
)

func newOrcaTestMux(t *testing.T, ts store.TenantStore) (*http.ServeMux, *memOrcaStore) {
	t.Helper()
	InitGatewayToken(orcaTestToken)
	t.Cleanup(func() { InitGatewayToken("") })
	ms := newMemOrcaStore()
	mux := http.NewServeMux()
	NewOrcaServersHandler(ms, ts, orcaserver.NewChecker()).RegisterRoutes(mux)
	return mux, ms
}

func orcaDo(mux *http.ServeMux, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+orcaTestToken)
	req.Header.Set("X-GoClaw-User-Id", "alice")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func orcaCreate(t *testing.T, mux *http.ServeMux, label string) string {
	t.Helper()
	rec := orcaDo(mux, "POST", "/v1/orca-servers", `{"label":"`+label+`","description":"d","pairing_code":"`+orcaTestCode+`"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", label, rec.Code, rec.Body)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func TestOrcaServersCRUDNeverReturnsPairingCode(t *testing.T) {
	mux, _ := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "Prod")

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/v1/orca-servers", ""},
		{"GET", "/v1/orca-servers/" + id, ""},
		{"PUT", "/v1/orca-servers/" + id, `{"description":"new","pairing_code":"orca://pair?rotated-secret"}`},
	} {
		rec := orcaDo(mux, tc.method, tc.path, tc.body, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
		body := rec.Body.String()
		if strings.Contains(body, `"pairing_code"`) || strings.Contains(body, "secret") {
			t.Fatalf("%s %s leaked pairing code: %s", tc.method, tc.path, body)
		}
		if !strings.Contains(body, `"has_pairing_code":true`) || !strings.Contains(body, `"label":"prod"`) {
			t.Fatalf("%s %s unexpected body: %s", tc.method, tc.path, body)
		}
	}

	if rec := orcaDo(mux, "DELETE", "/v1/orca-servers/"+id, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := orcaDo(mux, "GET", "/v1/orca-servers/"+id, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", rec.Code)
	}
}

func TestOrcaServersUpdateWithEmptyCodeKeepsExistingCode(t *testing.T) {
	mux, ms := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "prod")
	if rec := orcaDo(mux, "PUT", "/v1/orca-servers/"+id, `{"description":"x","pairing_code":""}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	for _, r := range ms.rows {
		if r.PairingCode != orcaTestCode {
			t.Fatalf("pairing code changed: %q", r.PairingCode)
		}
	}
}

func TestOrcaServersCreateValidation(t *testing.T) {
	mux, _ := newOrcaTestMux(t, oauthAdminTS{})
	orcaCreate(t, mux, "prod")
	cases := []struct {
		name, body string
		want       int
	}{
		{"empty label", `{"label":"","pairing_code":"` + orcaTestCode + `"}`, 400},
		{"reserved label", `{"label":"default","pairing_code":"` + orcaTestCode + `"}`, 400},
		{"bad label chars", `{"label":"a b!","pairing_code":"` + orcaTestCode + `"}`, 400},
		{"missing prefix", `{"label":"x","pairing_code":"https://nope"}`, 400},
		{"empty code", `{"label":"x","pairing_code":""}`, 400},
		{"long description", `{"label":"x","description":"` + strings.Repeat("a", 201) + `","pairing_code":"` + orcaTestCode + `"}`, 400},
		{"malformed json", `{`, 400},
		{"duplicate label", `{"label":"PROD","pairing_code":"` + orcaTestCode + `"}`, 409},
	}
	for _, tc := range cases {
		rec := orcaDo(mux, "POST", "/v1/orca-servers", tc.body, nil)
		if rec.Code != tc.want {
			t.Errorf("%s: got %d want %d (%s)", tc.name, rec.Code, tc.want, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "very-secret-payload") {
			t.Errorf("%s: response echoed pairing code", tc.name)
		}
	}
}

func TestOrcaServersCreateEnforcesTenantLimit(t *testing.T) {
	mux, _ := newOrcaTestMux(t, oauthAdminTS{})
	for i := 0; i < store.OrcaServerMaxPerTenant; i++ {
		orcaCreate(t, mux, "s"+strings.Repeat("a", i))
	}
	rec := orcaDo(mux, "POST", "/v1/orca-servers", `{"label":"over","pairing_code":"`+orcaTestCode+`"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit: %d %s", rec.Code, rec.Body)
	}
}

func TestOrcaServersRejectNonTenantAdminAndAnonymous(t *testing.T) {
	mux, _ := newOrcaTestMux(t, oauthDenyTS{})
	for _, p := range []struct{ method, path string }{
		{"GET", "/v1/orca-servers"},
		{"POST", "/v1/orca-servers"},
		{"GET", "/v1/orca-servers/" + uuid.NewString()},
		{"PUT", "/v1/orca-servers/" + uuid.NewString()},
		{"DELETE", "/v1/orca-servers/" + uuid.NewString()},
		{"POST", "/v1/orca-servers/" + uuid.NewString() + "/check"},
	} {
		if rec := orcaDo(mux, p.method, p.path, `{}`, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s non-admin: %d", p.method, p.path, rec.Code)
		}
		if rec := orcaDo(mux, p.method, p.path, `{}`, map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous: %d", p.method, p.path, rec.Code)
		}
	}
}

func TestOrcaServersOtherTenantRowIsNotFound(t *testing.T) {
	mux, ms := newOrcaTestMux(t, oauthAdminTS{})
	foreign := &store.OrcaServer{ID: uuid.New(), TenantID: uuid.New(), Label: "x", PairingCode: orcaTestCode}
	ms.rows[foreign.ID] = foreign
	for _, p := range []struct{ method, path string }{
		{"GET", "/v1/orca-servers/" + foreign.ID.String()},
		{"DELETE", "/v1/orca-servers/" + foreign.ID.String()},
		{"POST", "/v1/orca-servers/" + foreign.ID.String() + "/check"},
	} {
		if rec := orcaDo(mux, p.method, p.path, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", p.method, p.path, rec.Code)
		}
	}
	if rec := orcaDo(mux, "GET", "/v1/orca-servers/not-a-uuid", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", rec.Code)
	}
}

func TestOrcaServersCheckNowPersistsHealth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orca"), []byte("#!/bin/sh\necho '{\"result\":{\"runtime\":{\"state\":\"ready\"}}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	mux, ms := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "prod")

	rec := orcaDo(mux, "POST", "/v1/orca-servers/"+id+"/check", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"last_status":"healthy"`) || !strings.Contains(body, `"last_runtime_state":"ready"`) || strings.Contains(body, `"pairing_code"`) {
		t.Fatalf("unexpected check body: %s", body)
	}
	for _, r := range ms.rows {
		if r.LastStatus != store.OrcaServerStatusHealthy || r.LastCheckedAt == nil {
			t.Fatalf("health not persisted: %+v", r)
		}
	}
}

func TestOrcaServersCheckNowWithoutCLIReturnsCLIMissingStatus(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mux, _ := newOrcaTestMux(t, oauthAdminTS{})
	id := orcaCreate(t, mux, "prod")
	rec := orcaDo(mux, "POST", "/v1/orca-servers/"+id+"/check", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"last_status":"cli_missing"`) {
		t.Fatalf("check without CLI: %d %s", rec.Code, rec.Body)
	}
}
