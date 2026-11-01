package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type labeledAgentSecureCLIStore struct {
	labeledMemSecureCLIStore
	agentRows map[string]*store.SecureCLIAgentCredential
}

func newLabeledAgentStore() *labeledAgentSecureCLIStore {
	return &labeledAgentSecureCLIStore{
		labeledMemSecureCLIStore: labeledMemSecureCLIStore{rows: map[string]*store.SecureCLIUserCredential{}},
		agentRows:                map[string]*store.SecureCLIAgentCredential{},
	}
}

func agentCredRowKey(binaryID, agentID uuid.UUID, label string) string {
	return binaryID.String() + "\x00" + agentID.String() + "\x00" + label
}

func (s *labeledAgentSecureCLIStore) BinaryExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *labeledAgentSecureCLIStore) AgentExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *labeledAgentSecureCLIStore) GetAgentCredentials(_ context.Context, binaryID, agentID uuid.UUID) (*store.SecureCLIAgentCredential, error) {
	return s.agentRows[agentCredRowKey(binaryID, agentID, "")], nil
}

func (s *labeledAgentSecureCLIStore) SetAgentCredentials(ctx context.Context, binaryID, agentID uuid.UUID, encryptedEnv []byte, createdBy string) error {
	return s.SetAgentCredentialsTyped(ctx, binaryID, agentID, encryptedEnv, nil, nil, createdBy)
}

func (s *labeledAgentSecureCLIStore) SetAgentCredentialsTyped(_ context.Context, binaryID, agentID uuid.UUID, encryptedEnv []byte, credentialType, hostScope *string, createdBy string) error {
	s.agentRows[agentCredRowKey(binaryID, agentID, "")] = &store.SecureCLIAgentCredential{
		BinaryID: binaryID, AgentID: agentID, EncryptedEnv: encryptedEnv,
		CredentialType: credentialType, HostScope: hostScope, CreatedBy: createdBy,
	}
	return nil
}

func (s *labeledAgentSecureCLIStore) DeleteAgentCredentials(_ context.Context, binaryID, agentID uuid.UUID) error {
	delete(s.agentRows, agentCredRowKey(binaryID, agentID, ""))
	return nil
}

func (s *labeledAgentSecureCLIStore) ListAgentCredentials(_ context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentCredential, error) {
	out := make([]store.SecureCLIAgentCredential, 0, len(s.agentRows))
	for _, c := range s.agentRows {
		if c.BinaryID == binaryID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (s *labeledAgentSecureCLIStore) ListCredentialLabels(_ context.Context, q store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	var out []store.SecureCLICredentialLabel
	for _, c := range s.agentRows {
		if q.BinaryID != nil && c.BinaryID != *q.BinaryID {
			continue
		}
		if q.AgentID != nil && c.AgentID != *q.AgentID {
			continue
		}
		out = append(out, store.SecureCLICredentialLabel{
			BinaryID: c.BinaryID, Label: c.Label, Description: c.Description, Source: "agent",
		})
	}
	return out, nil
}

func (s *labeledAgentSecureCLIStore) GetAgentCredentialByLabel(_ context.Context, binaryID, agentID uuid.UUID, label string) (*store.SecureCLIAgentCredential, error) {
	return s.agentRows[agentCredRowKey(binaryID, agentID, label)], nil
}

func (s *labeledAgentSecureCLIStore) SetAgentCredentialLabeled(_ context.Context, binaryID, agentID uuid.UUID, w store.SecureCLICredentialWrite, createdBy string) error {
	s.agentRows[agentCredRowKey(binaryID, agentID, w.Label)] = &store.SecureCLIAgentCredential{
		BinaryID: binaryID, AgentID: agentID, Label: w.Label, Description: w.Description,
		EncryptedEnv: w.Env, CredentialType: w.CredentialType, HostScope: w.HostScope, CreatedBy: createdBy,
	}
	return nil
}

func (s *labeledAgentSecureCLIStore) DeleteAgentCredentialByLabel(_ context.Context, binaryID, agentID uuid.UUID, label string) error {
	delete(s.agentRows, agentCredRowKey(binaryID, agentID, label))
	return nil
}

type agentCredCall func(h *SecureCLIHandler, w http.ResponseWriter, r *http.Request)

type agentCredCase struct {
	t      *testing.T
	st     *labeledAgentSecureCLIStore
	h      *SecureCLIHandler
	binary uuid.UUID
}

func newAgentCredCase(t *testing.T) *agentCredCase {
	t.Helper()
	st := newLabeledAgentStore()
	return &agentCredCase{t: t, st: st, h: newLabeledHandler(st), binary: uuid.New()}
}

func (c *agentCredCase) do(call agentCredCall, method string, agent uuid.UUID, rawQuery string, body any, locale string) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	target := "/v1/cli-credentials/" + c.binary.String() + "/agent-credentials"
	if agent != uuid.Nil {
		target += "/" + agent.String()
	}
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(buf))
	req.SetPathValue("id", c.binary.String())
	if agent != uuid.Nil {
		req.SetPathValue("agentId", agent.String())
	}
	req = withTenantAdminContext(req)
	if locale != "" {
		req = req.WithContext(store.WithLocale(req.Context(), locale))
	}
	rec := httptest.NewRecorder()
	call(c.h, rec, req)
	return rec
}

func agentCredLabelQuery(label string) string {
	if label == "" {
		return ""
	}
	return url.Values{"label": {label}}.Encode()
}

func (c *agentCredCase) put(agent uuid.UUID, label string, body any) *httptest.ResponseRecorder {
	return c.do((*SecureCLIHandler).handleSetAgentCredentials, http.MethodPut, agent, agentCredLabelQuery(label), body, "")
}

func (c *agentCredCase) get(agent uuid.UUID, label string) *httptest.ResponseRecorder {
	return c.do((*SecureCLIHandler).handleGetAgentCredentials, http.MethodGet, agent, agentCredLabelQuery(label), nil, "")
}

func (c *agentCredCase) del(agent uuid.UUID, label string) *httptest.ResponseRecorder {
	return c.do((*SecureCLIHandler).handleDeleteAgentCredentials, http.MethodDelete, agent, agentCredLabelQuery(label), nil, "")
}

func (c *agentCredCase) list() *httptest.ResponseRecorder {
	return c.do((*SecureCLIHandler).handleListAgentCredentials, http.MethodGet, uuid.Nil, "", nil, "")
}

func (c *agentCredCase) row(agent uuid.UUID, label string) *store.SecureCLIAgentCredential {
	return c.st.agentRows[agentCredRowKey(c.binary, agent, label)]
}

func agentCredEnvBody(key, value string) map[string]any {
	return map[string]any{"env": map[string]string{key: value}}
}

type agentCredListEntry struct {
	AgentID     string `json:"agent_id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func decodeAgentCredList(t *testing.T, rec *httptest.ResponseRecorder) []agentCredListEntry {
	t.Helper()
	var resp struct {
		Creds []agentCredListEntry `json:"agent_credentials"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v body=%s", err, rec.Body.String())
	}
	return resp.Creds
}

func TestAgentCredentialLabeledLifecycle(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	for _, label := range []string{"a", "B"} {
		rec := c.put(agent, label, map[string]any{"env": map[string]string{"ORCA_PAIRING_CODE": "code-" + label}, "description": "vm " + label})
		if rec.Code != http.StatusOK {
			t.Fatalf("put %s status=%d body=%s", label, rec.Code, rec.Body.String())
		}
	}
	if rec := c.put(agent, "", agentCredEnvBody("TOKEN", "x")); rec.Code != http.StatusOK {
		t.Fatalf("legacy put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "") == nil || c.row(agent, "b") == nil || c.row(agent, "B") != nil {
		t.Fatalf("rows keyed wrongly: %v", c.st.agentRows)
	}

	rec := c.list()
	entries := decodeAgentCredList(t, rec)
	if len(entries) != 3 || entries[0].Label != "" || entries[1].Label != "a" || entries[2].Label != "b" || entries[2].Description != "vm B" {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}

	if rec := c.del(agent, "a"); rec.Code != http.StatusOK {
		t.Fatalf("delete a status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "a") != nil || c.row(agent, "b") == nil || c.row(agent, "") == nil {
		t.Fatalf("delete label a wrong: rows=%v", c.st.agentRows)
	}

	rec = c.get(agent, "b")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"label":"b"`) {
		t.Fatalf("get label b: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.get(agent, "missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("get missing label status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentCredentialLabelNormalization(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	if rec := c.put(agent, "  Default ", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK || c.row(agent, "") == nil {
		t.Fatalf("default alias put status=%d body=%s rows=%v", rec.Code, rec.Body.String(), c.st.agentRows)
	}
	if rec := c.put(agent, "  MiXeD ", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
		t.Fatalf("mixed-case put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "mixed") == nil || c.row(agent, "MiXeD") != nil {
		t.Fatalf("label not lowercased: rows=%v", c.st.agentRows)
	}
	if rec := c.get(agent, "DEFAULT"); rec.Code != http.StatusOK {
		t.Fatalf("DEFAULT alias get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.get(agent, "MIXED"); rec.Code != http.StatusOK {
		t.Fatalf("MIXED get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.del(agent, "default"); rec.Code != http.StatusOK || c.row(agent, "") != nil || c.row(agent, "mixed") == nil {
		t.Fatalf("default alias delete wrong: status=%d rows=%v", rec.Code, c.st.agentRows)
	}
}

func TestAgentCredentialInvalidLabelRejected(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	for _, raw := range []string{"bad label!", "-leading", "_leading", "a/b", "a.b", strings.Repeat("a", 33)} {
		if rec := c.put(agent, raw, agentCredEnvBody("A", "b")); rec.Code != http.StatusBadRequest {
			t.Fatalf("put %q status=%d body=%s", raw, rec.Code, rec.Body.String())
		}
		if rec := c.get(agent, raw); rec.Code != http.StatusBadRequest {
			t.Fatalf("get %q status=%d body=%s", raw, rec.Code, rec.Body.String())
		}
		if rec := c.del(agent, raw); rec.Code != http.StatusBadRequest {
			t.Fatalf("delete %q status=%d body=%s", raw, rec.Code, rec.Body.String())
		}
	}
	if len(c.st.agentRows) != 0 {
		t.Fatalf("invalid label wrote rows: %v", c.st.agentRows)
	}

	maxLen := strings.Repeat("a", 32)
	if rec := c.put(agent, maxLen, agentCredEnvBody("A", "b")); rec.Code != http.StatusOK || c.row(agent, maxLen) == nil {
		t.Fatalf("32-char label status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec := c.do((*SecureCLIHandler).handleSetAgentCredentials, http.MethodPut, agent, agentCredLabelQuery("bad label!"), agentCredEnvBody("A", "b"), "vi")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "không hợp lệ") {
		t.Fatalf("vi localized error status=%d body=%s", rec.Code, rec.Body.String())
	}

	long := strings.Repeat("x", 200) + "!"
	rec = c.put(agent, long, agentCredEnvBody("A", "b"))
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), strings.Repeat("x", 65)) {
		t.Fatalf("long label must be rejected and truncated in message: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentCredentialLabelLimitPerAgent(t *testing.T) {
	c := newAgentCredCase(t)
	a1, a2 := uuid.New(), uuid.New()

	for i := 0; i < maxSecureCLICredentialLabels; i++ {
		label := "l" + string(rune('a'+i))
		if rec := c.put(a1, label, agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
			t.Fatalf("label %s status=%d body=%s", label, rec.Code, rec.Body.String())
		}
	}
	rec := c.put(a1, "overflow", agentCredEnvBody("A", "b"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too many server labels") {
		t.Fatalf("21st label status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(a1, "overflow") != nil {
		t.Fatalf("21st label must not be stored")
	}
	if rec := c.put(a1, "la", agentCredEnvBody("A", "c")); rec.Code != http.StatusOK {
		t.Fatalf("updating existing label at cap status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.put(a2, "overflow", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
		t.Fatalf("cap must be per agent, other agent status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentCredentialDescriptionRules(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	tooLong := map[string]any{"env": map[string]string{"A": "b"}, "description": strings.Repeat("d", 201)}
	if rec := c.put(agent, "vm", tooLong); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "description too long") {
		t.Fatalf("too long description status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "vm") != nil {
		t.Fatalf("rejected description must not create row")
	}
	if rec := c.put(agent, "vm", map[string]any{"description": "only"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("description-only on missing label status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.put(agent, "", map[string]any{"description": "only"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("description-only on missing default status=%d body=%s", rec.Code, rec.Body.String())
	}

	exact := strings.Repeat("é", 200)
	if rec := c.put(agent, "vm", map[string]any{"env": map[string]string{"A": "b"}, "description": exact}); rec.Code != http.StatusOK {
		t.Fatalf("200-rune multibyte description status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := c.row(agent, "vm").Description; got != exact {
		t.Fatalf("200-rune description altered: len=%d", len([]rune(got)))
	}
	if rec := c.put(agent, "vm", map[string]any{"description": strings.Repeat("é", 201)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("201-rune multibyte description status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := c.put(agent, "vm", map[string]any{"env": map[string]string{"A": "b"}, "description": "first\n## SYSTEM\r\tx"}); rec.Code != http.StatusOK {
		t.Fatalf("sanitized description status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := c.row(agent, "vm").Description; got != "first ## SYSTEM  x" {
		t.Fatalf("description not sanitized: %q", got)
	}

	envBefore := string(c.row(agent, "vm").EncryptedEnv)
	if rec := c.put(agent, "vm", map[string]any{"description": "renamed"}); rec.Code != http.StatusOK {
		t.Fatalf("description-only status=%d body=%s", rec.Code, rec.Body.String())
	}
	row := c.row(agent, "vm")
	if row.Description != "renamed" || string(row.EncryptedEnv) != envBefore {
		t.Fatalf("description-only must keep env: %+v", row)
	}

	if rec := c.put(agent, "vm", agentCredEnvBody("C", "d")); rec.Code != http.StatusOK {
		t.Fatalf("env update status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := c.row(agent, "vm").Description; got != "renamed" {
		t.Fatalf("env update must keep description, got %q", got)
	}
}

func TestAgentCredentialLabeledTypedHidesBlob(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	rec := c.put(agent, "gh", map[string]any{
		"credential_type": "pat", "host_scope": "github.com",
		"blob": map[string]string{"token": "ghp_labeledSecret123456"}, "description": "work",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("typed put status=%d body=%s", rec.Code, rec.Body.String())
	}
	row := c.row(agent, "gh")
	if row == nil || row.CredentialType == nil || *row.CredentialType != "pat" || row.HostScope == nil || *row.HostScope != "github.com" {
		t.Fatalf("typed fields not stored: %+v", row)
	}

	rec = c.get(agent, "gh")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "ghp_labeledSecret") || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("typed labeled get leaked: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = c.list()
	if strings.Contains(rec.Body.String(), "ghp_labeledSecret") {
		t.Fatalf("list leaked: %s", rec.Body.String())
	}
	var raw struct {
		Creds []map[string]json.RawMessage `json:"agent_credentials"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Creds) != 1 {
		t.Fatalf("expected 1 entry: %s", rec.Body.String())
	}
	for _, key := range []string{"env", "env_keys"} {
		if _, ok := raw.Creds[0][key]; ok {
			t.Fatalf("typed list entry must omit %q: %s", key, rec.Body.String())
		}
	}

	envBefore := string(row.EncryptedEnv)
	if rec := c.put(agent, "gh", map[string]any{"description": "personal"}); rec.Code != http.StatusOK {
		t.Fatalf("typed description-only status=%d body=%s", rec.Code, rec.Body.String())
	}
	row = c.row(agent, "gh")
	if row.Description != "personal" || string(row.EncryptedEnv) != envBefore ||
		row.CredentialType == nil || *row.CredentialType != "pat" || row.HostScope == nil || *row.HostScope != "github.com" {
		t.Fatalf("description-only must keep typed env/type/host: %+v", row)
	}
}

func TestAgentCredentialListSortedByAgentThenLabel(t *testing.T) {
	c := newAgentCredCase(t)
	a1, a2 := uuid.New(), uuid.New()
	if a2.String() < a1.String() {
		a1, a2 = a2, a1
	}
	steps := []struct {
		agent uuid.UUID
		label string
	}{{a2, "zeta"}, {a1, "b"}, {a2, "alpha"}, {a1, ""}, {a1, "a"}}
	for _, s := range steps {
		if rec := c.put(s.agent, s.label, agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
			t.Fatalf("put %s/%q status=%d body=%s", s.agent, s.label, rec.Code, rec.Body.String())
		}
	}

	entries := decodeAgentCredList(t, c.list())
	want := []struct {
		agent uuid.UUID
		label string
	}{{a1, ""}, {a1, "a"}, {a1, "b"}, {a2, "alpha"}, {a2, "zeta"}}
	if len(entries) != len(want) {
		t.Fatalf("list len=%d want=%d entries=%+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		if entries[i].AgentID != w.agent.String() || entries[i].Label != w.label {
			t.Fatalf("entry %d = %s/%q want %s/%q", i, entries[i].AgentID, entries[i].Label, w.agent, w.label)
		}
	}
}

func TestAgentCredentialDeleteWithoutLabelTargetsDefaultOnly(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()
	if rec := c.put(agent, "", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
		t.Fatalf("default put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.put(agent, "a", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
		t.Fatalf("label a put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.del(agent, ""); rec.Code != http.StatusOK {
		t.Fatalf("default delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "") != nil || c.row(agent, "a") == nil {
		t.Fatalf("default delete must keep labeled rows: %v", c.st.agentRows)
	}
}

func TestAgentCredentialLegacyPutUnchanged(t *testing.T) {
	c := newAgentCredCase(t)
	agent := uuid.New()

	if rec := c.put(agent, "", agentCredEnvBody("TOKEN", "x")); rec.Code != http.StatusOK {
		t.Fatalf("legacy env put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c.row(agent, "") == nil || len(c.st.agentRows) != 1 {
		t.Fatalf("legacy put must write only default row: %v", c.st.agentRows)
	}
	if rec := c.get(agent, ""); rec.Code != http.StatusOK {
		t.Fatalf("legacy get status=%d body=%s", rec.Code, rec.Body.String())
	}

	typedAgent := uuid.New()
	rec := c.put(typedAgent, "", map[string]any{
		"credential_type": "pat", "host_scope": "github.com",
		"blob": map[string]string{"token": "ghp_legacySecret123456"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy typed put status=%d body=%s", rec.Code, rec.Body.String())
	}
	row := c.row(typedAgent, "")
	if row == nil || row.CredentialType == nil || *row.CredentialType != "pat" || row.Label != "" {
		t.Fatalf("legacy typed put wrong row: %+v", row)
	}
}

func TestAgentCredentialLabelRequiresLabeledStore(t *testing.T) {
	c := &agentCredCase{t: t, h: newLabeledHandler(&recordingSecureCLIStore{}), binary: uuid.New()}
	agent := uuid.New()

	if rec := c.put(agent, "vm", agentCredEnvBody("A", "b")); rec.Code != http.StatusNotImplemented {
		t.Fatalf("labeled put status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.get(agent, "vm"); rec.Code != http.StatusNotImplemented {
		t.Fatalf("labeled get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.del(agent, "vm"); rec.Code != http.StatusNotImplemented {
		t.Fatalf("labeled delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := c.put(agent, "", agentCredEnvBody("A", "b")); rec.Code != http.StatusOK {
		t.Fatalf("unlabeled put must keep working on plain store, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentCredentialRoutesRequireTenantAdmin(t *testing.T) {
	oldToken, oldFallback := pkgGatewayToken, pkgNoAuthFallbackAllowed
	pkgGatewayToken, pkgNoAuthFallbackAllowed = "", true
	defer func() { pkgGatewayToken, pkgNoAuthFallbackAllowed = oldToken, oldFallback }()

	st := newLabeledAgentStore()
	ts := newMockTenantStore()
	ts.addTenant(store.MasterTenantID, "master")
	ts.setUserRole(store.MasterTenantID, "system", store.TenantRoleViewer)
	h := NewSecureCLIHandler(st, nil, ts)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	base := "/v1/cli-credentials/" + uuid.New().String() + "/agent-credentials"
	agent := uuid.New().String()
	cases := []struct{ method, path, body string }{
		{http.MethodGet, base, ""},
		{http.MethodGet, base + "/" + agent + "?label=vm", ""},
		{http.MethodPut, base + "/" + agent + "?label=vm", `{"env":{"A":"b"}}`},
		{http.MethodPut, base + "/" + agent, `{"env":{"A":"b"}}`},
		{http.MethodDelete, base + "/" + agent + "?label=vm", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("X-GoClaw-User-Id", "browser-user")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if len(st.agentRows) != 0 {
		t.Fatalf("viewer wrote rows: %v", st.agentRows)
	}
}
