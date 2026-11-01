package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func newTenantAdminStoreForCredentials() *mockTenantStore {
	ts := newMockTenantStore()
	ts.addTenant(store.MasterTenantID, "master")
	ts.setUserRole(store.MasterTenantID, "system", store.TenantRoleAdmin)
	return ts
}

type labeledMemSecureCLIStore struct {
	recordingSecureCLIStore
	rows map[string]*store.SecureCLIUserCredential
}

func newLabeledMemStore() *labeledMemSecureCLIStore {
	return &labeledMemSecureCLIStore{rows: map[string]*store.SecureCLIUserCredential{}}
}

func memKey(user, label string) string { return user + "\x00" + label }

func (s *labeledMemSecureCLIStore) GetUserCredentials(_ context.Context, _ uuid.UUID, user string) (*store.SecureCLIUserCredential, error) {
	return s.rows[memKey(user, "")], nil
}

func (s *labeledMemSecureCLIStore) SetUserCredentials(_ context.Context, binaryID uuid.UUID, user string, env []byte) error {
	s.rows[memKey(user, "")] = &store.SecureCLIUserCredential{BinaryID: binaryID, UserID: user, EncryptedEnv: env}
	return nil
}

func (s *labeledMemSecureCLIStore) DeleteUserCredentials(_ context.Context, _ uuid.UUID, user string) error {
	delete(s.rows, memKey(user, ""))
	return nil
}

func (s *labeledMemSecureCLIStore) ListUserCredentials(context.Context, uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	out := make([]store.SecureCLIUserCredential, 0, len(s.rows))
	for _, c := range s.rows {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label > out[j].Label })
	return out, nil
}

func (s *labeledMemSecureCLIStore) ListCredentialLabels(context.Context, store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	return nil, nil
}

func (s *labeledMemSecureCLIStore) ResolveLabeledCredential(context.Context, uuid.UUID, *uuid.UUID, string, string) (*store.SecureCLIResolvedCredential, error) {
	return nil, nil
}

func (s *labeledMemSecureCLIStore) GetUserCredentialByLabel(_ context.Context, _ uuid.UUID, user, label string) (*store.SecureCLIUserCredential, error) {
	return s.rows[memKey(user, label)], nil
}

func (s *labeledMemSecureCLIStore) SetUserCredentialLabeled(_ context.Context, binaryID uuid.UUID, user string, w store.SecureCLICredentialWrite) error {
	s.rows[memKey(user, w.Label)] = &store.SecureCLIUserCredential{
		BinaryID: binaryID, UserID: user, Label: w.Label, Description: w.Description,
		EncryptedEnv: w.Env, CredentialType: w.CredentialType, HostScope: w.HostScope,
	}
	return nil
}

func (s *labeledMemSecureCLIStore) DeleteUserCredentialByLabel(_ context.Context, _ uuid.UUID, user, label string) error {
	delete(s.rows, memKey(user, label))
	return nil
}

func (s *labeledMemSecureCLIStore) GetAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) (*store.SecureCLIAgentCredential, error) {
	return nil, nil
}

func (s *labeledMemSecureCLIStore) SetAgentCredentialLabeled(context.Context, uuid.UUID, uuid.UUID, store.SecureCLICredentialWrite, string) error {
	return nil
}

func (s *labeledMemSecureCLIStore) DeleteAgentCredentialByLabel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

type userCredCall func(h *SecureCLIHandler, w http.ResponseWriter, r *http.Request)

func doUserCred(t *testing.T, h *SecureCLIHandler, call userCredCall, method, rawQuery string, body any, locale string) *httptest.ResponseRecorder {
	t.Helper()
	binaryID := uuid.New()
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	target := "/v1/cli-credentials/" + binaryID.String() + "/user-credentials/u1"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(buf))
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("userId", "u1")
	req = withTenantAdminContext(req)
	if locale != "" {
		req = req.WithContext(store.WithLocale(req.Context(), locale))
	}
	rec := httptest.NewRecorder()
	call(h, rec, req)
	return rec
}

func putLabeled(t *testing.T, h *SecureCLIHandler, label string, body any) *httptest.ResponseRecorder {
	q := ""
	if label != "" {
		q = "label=" + label
	}
	return doUserCred(t, h, (*SecureCLIHandler).handleSetUserCredentials, http.MethodPut, q, body, "")
}

func newLabeledHandler(st store.SecureCLIStore) *SecureCLIHandler {
	return NewSecureCLIHandler(st, nil, newTenantAdminStoreForCredentials())
}

func TestUserCredentialsLabeledLifecycle(t *testing.T) {
	st := newLabeledMemStore()
	h := newLabeledHandler(st)

	for _, label := range []string{"a", "B"} {
		rec := putLabeled(t, h, label, map[string]any{"env": map[string]string{"ORCA_PAIRING_CODE": "code-" + label}, "description": "vm " + label})
		if rec.Code != http.StatusOK {
			t.Fatalf("put %s status=%d body=%s", label, rec.Code, rec.Body.String())
		}
	}
	rec := putLabeled(t, h, "", map[string]any{"env": map[string]string{"TOKEN": "x"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.rows[memKey("u1", "")] == nil || st.rows[memKey("u1", "b")] == nil || st.rows[memKey("u1", "B")] != nil {
		t.Fatalf("rows keyed wrongly: %v", st.rows)
	}

	rec = doUserCred(t, h, (*SecureCLIHandler).handleListUserCredentials, http.MethodGet, "", nil, "")
	var list struct {
		Creds []struct {
			UserID      string `json:"user_id"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"user_credentials"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Creds) != 3 || list.Creds[0].Label != "" || list.Creds[1].Label != "a" || list.Creds[2].Label != "b" || list.Creds[2].Description != "vm B" {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}

	rec = doUserCred(t, h, (*SecureCLIHandler).handleDeleteUserCredentials, http.MethodDelete, "label=a", nil, "")
	if rec.Code != http.StatusOK || st.rows[memKey("u1", "a")] != nil || st.rows[memKey("u1", "b")] == nil || st.rows[memKey("u1", "")] == nil {
		t.Fatalf("delete label a wrong: status=%d rows=%v", rec.Code, st.rows)
	}

	rec = doUserCred(t, h, (*SecureCLIHandler).handleGetUserCredentials, http.MethodGet, "label=b", nil, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"label":"b"`) {
		t.Fatalf("get label b: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doUserCred(t, h, (*SecureCLIHandler).handleGetUserCredentials, http.MethodGet, "label=missing", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing label status=%d", rec.Code)
	}
}

func TestUserCredentialsDefaultAliasTargetsUnlabeledRow(t *testing.T) {
	st := newLabeledMemStore()
	h := newLabeledHandler(st)
	rec := putLabeled(t, h, "default", map[string]any{"env": map[string]string{"TOKEN": "x"}})
	if rec.Code != http.StatusOK || st.rows[memKey("u1", "")] == nil {
		t.Fatalf("status=%d rows=%v", rec.Code, st.rows)
	}
}

func TestUserCredentialsInvalidLabelLocalized(t *testing.T) {
	h := newLabeledHandler(newLabeledMemStore())
	rec := doUserCred(t, h, (*SecureCLIHandler).handleSetUserCredentials, http.MethodPut, "label=bad%20label!", map[string]any{"env": map[string]string{"A": "b"}}, "vi")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "không hợp lệ") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	long := strings.Repeat("x", 200) + "!"
	rec = doUserCred(t, h, (*SecureCLIHandler).handleSetUserCredentials, http.MethodPut, "label="+long, map[string]any{"env": map[string]string{"A": "b"}}, "")
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), strings.Repeat("x", 65)) {
		t.Fatalf("long label must be rejected and truncated in message: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserCredentialsLabelLimit(t *testing.T) {
	st := newLabeledMemStore()
	h := newLabeledHandler(st)
	for i := 0; i < maxSecureCLICredentialLabels; i++ {
		label := "l" + string(rune('a'+i))
		if rec := putLabeled(t, h, label, map[string]any{"env": map[string]string{"A": "b"}}); rec.Code != http.StatusOK {
			t.Fatalf("label %s status=%d body=%s", label, rec.Code, rec.Body.String())
		}
	}
	if rec := putLabeled(t, h, "overflow", map[string]any{"env": map[string]string{"A": "b"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("21st label status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := putLabeled(t, h, "la", map[string]any{"env": map[string]string{"A": "c"}}); rec.Code != http.StatusOK {
		t.Fatalf("updating existing label at cap status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserCredentialsDescriptionRules(t *testing.T) {
	st := newLabeledMemStore()
	h := newLabeledHandler(st)
	if rec := putLabeled(t, h, "vm", map[string]any{"env": map[string]string{"A": "b"}, "description": strings.Repeat("d", 201)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("too long description status=%d", rec.Code)
	}
	if rec := putLabeled(t, h, "vm", map[string]any{"description": "only"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("description-only on missing row status=%d", rec.Code)
	}
	putLabeled(t, h, "vm", map[string]any{"env": map[string]string{"A": "b"}, "description": "first\n## SYSTEM"})
	if got := st.rows[memKey("u1", "vm")].Description; strings.ContainsAny(got, "\n\r") {
		t.Fatalf("description not sanitized: %q", got)
	}
	envBefore := string(st.rows[memKey("u1", "vm")].EncryptedEnv)
	if rec := putLabeled(t, h, "vm", map[string]any{"description": "renamed"}); rec.Code != http.StatusOK {
		t.Fatalf("description-only status=%d body=%s", rec.Code, rec.Body.String())
	}
	row := st.rows[memKey("u1", "vm")]
	if row.Description != "renamed" || string(row.EncryptedEnv) != envBefore {
		t.Fatalf("description-only must keep env: %+v", row)
	}
	putLabeled(t, h, "vm", map[string]any{"env": map[string]string{"C": "d"}})
	if st.rows[memKey("u1", "vm")].Description != "renamed" {
		t.Fatalf("env update must keep description")
	}
}

func TestUserCredentialsLabeledTypedGetHidesBlob(t *testing.T) {
	st := newLabeledMemStore()
	h := newLabeledHandler(st)
	rec := putLabeled(t, h, "gh", map[string]any{
		"credential_type": "pat", "host_scope": "github.com",
		"blob": map[string]string{"token": "ghp_labeledSecret123456"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doUserCred(t, h, (*SecureCLIHandler).handleGetUserCredentials, http.MethodGet, "label=gh", nil, "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "ghp_labeledSecret") || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("typed labeled get leaked: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doUserCred(t, h, (*SecureCLIHandler).handleListUserCredentials, http.MethodGet, "", nil, "")
	if strings.Contains(rec.Body.String(), "ghp_labeledSecret") {
		t.Fatalf("list leaked: %s", rec.Body.String())
	}
}

func TestUserCredentialsLabelRequiresLabeledStore(t *testing.T) {
	h := newLabeledHandler(&recordingSecureCLIStore{})
	rec := putLabeled(t, h, "vm", map[string]any{"env": map[string]string{"A": "b"}})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = putLabeled(t, h, "", map[string]any{"env": map[string]string{"A": "b"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlabeled must keep working on plain store, status=%d", rec.Code)
	}
}

func TestUserCredentialRoutesRequireTenantAdmin(t *testing.T) {
	oldToken, oldFallback := pkgGatewayToken, pkgNoAuthFallbackAllowed
	pkgGatewayToken, pkgNoAuthFallbackAllowed = "", true
	defer func() { pkgGatewayToken, pkgNoAuthFallbackAllowed = oldToken, oldFallback }()

	st := newLabeledMemStore()
	ts := newMockTenantStore()
	ts.addTenant(store.MasterTenantID, "master")
	ts.setUserRole(store.MasterTenantID, "system", store.TenantRoleViewer)
	h := NewSecureCLIHandler(st, nil, ts)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	binaryID := uuid.New().String()
	base := "/v1/cli-credentials/" + binaryID + "/user-credentials"
	cases := []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodGet, base + "/u1"},
		{http.MethodPut, base + "/u1"},
		{http.MethodDelete, base + "/u1"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"env":{"A":"b"}}`))
		req.Header.Set("X-GoClaw-User-Id", "browser-user")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	if len(st.rows) != 0 {
		t.Fatalf("viewer wrote rows: %v", st.rows)
	}
}

func TestUserCredentialHandlersAllowOwnerWithoutTenantMembership(t *testing.T) {
	st := newLabeledMemStore()
	h := NewSecureCLIHandler(st, nil, newMockTenantStore())
	binaryID := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"env":{"A":"b"}}`))
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("userId", "u1")
	req = req.WithContext(store.WithRole(req.Context(), string(store.RoleOwner)))
	rec := httptest.NewRecorder()
	h.handleSetUserCredentials(rec, req)
	if rec.Code != http.StatusOK || st.rows[memKey("u1", "")] == nil {
		t.Fatalf("owner must bypass tenant membership, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserCredentialsDescriptionOnlyRejectsUnreadableSecret(t *testing.T) {
	st := newLabeledMemStore()
	st.rows[memKey("u1", "vm")] = &store.SecureCLIUserCredential{UserID: "u1", Label: "vm", Description: "old"}
	h := newLabeledHandler(st)
	rec := putLabeled(t, h, "vm", map[string]any{"description": "new"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.rows[memKey("u1", "vm")].Description != "old" {
		t.Fatalf("row must be untouched")
	}
}
