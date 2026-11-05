package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers/antigravity"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func newAgyTestServer(t *testing.T, providerType string) (*http.ServeMux, string, uuid.UUID) {
	t.Helper()
	token := setupProvidersAdminToken(t)
	ps := newMockProviderStore()
	id := uuid.New()
	ps.providers["agy"] = &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: id},
		TenantID:     store.MasterTenantID,
		Name:         "agy",
		ProviderType: providerType,
		Enabled:      true,
	}
	h := NewProvidersHandler(ps, newMockSecretsStore(), nil, "")
	h.SetAntigravityLoginManager(antigravity.NewLoginManagerWith(
		func(uuid.UUID, io.Writer) (*antigravity.Proc, error) { return nil, io.EOF },
		func(context.Context, uuid.UUID) (bool, error) { return false, nil },
		time.Minute,
	))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, token, id
}

func agyDo(mux *http.ServeMux, token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestAntigravityRoutes_WrongProviderType(t *testing.T) {
	mux, token, id := newAgyTestServer(t, store.ProviderOpenAICompat)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/v1/providers/" + id.String() + "/antigravity/login/start"},
		{http.MethodPost, "/v1/providers/" + id.String() + "/antigravity/login/code"},
		{http.MethodGet, "/v1/providers/" + id.String() + "/antigravity/auth-status"},
	} {
		if w := agyDo(mux, token, c.method, c.path, `{"code":"x"}`); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", c.method, c.path, w.Code)
		}
	}
}

func TestAntigravityRoutes_UnknownProvider(t *testing.T) {
	mux, token, _ := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers/"+uuid.NewString()+"/antigravity/login/start", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAntigravityLoginCode_NoSession(t *testing.T) {
	mux, token, id := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers/"+id.String()+"/antigravity/login/code", `{"code":"4/abc"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestAntigravityLoginCode_InvalidCode(t *testing.T) {
	mux, token, id := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers/"+id.String()+"/antigravity/login/code", `{"code":"a b\n"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestAntigravityLoginStart_RuntimeNotConfigured(t *testing.T) {
	t.Setenv(antigravity.BinaryEnv, "")
	mux, token, id := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers/"+id.String()+"/antigravity/login/start", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestProvidersUpdate_RejectsTypeChangeIntoOrOutOfAntigravity(t *testing.T) {
	for _, c := range []struct{ current, next string }{
		{store.ProviderAntigravityCLI, store.ProviderOpenAICompat},
		{store.ProviderOpenAICompat, store.ProviderAntigravityCLI},
	} {
		mux, token, id := newAgyTestServer(t, c.current)
		w := agyDo(mux, token, http.MethodPut, "/v1/providers/"+id.String(), `{"provider_type":"`+c.next+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s -> %s = %d, want 400", c.current, c.next, w.Code)
		}
	}
}

func TestProvidersCreate_AntigravityRejectsAPIBase(t *testing.T) {
	mux, token, _ := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers", `{"name":"agy2","provider_type":"antigravity_cli","api_base":"https://example.com","enabled":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "api_base") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestProvidersCreate_AntigravityAccepted(t *testing.T) {
	mux, token, _ := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers", `{"name":"agy2","provider_type":"antigravity_cli","enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestProvidersCreate_NameCollisionAcrossAntigravityTypeIs409(t *testing.T) {
	mux, token, _ := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers", `{"name":"agy","provider_type":"openai_compat","api_base":"https://api.openai.com/v1","api_key":"k","enabled":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("agy->openai: status = %d body = %s", w.Code, w.Body.String())
	}
	mux, token, _ = newAgyTestServer(t, store.ProviderOpenAICompat)
	w = agyDo(mux, token, http.MethodPost, "/v1/providers", `{"name":"agy","provider_type":"antigravity_cli","enabled":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("openai->agy: status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestProvidersCreate_RejectsInvalidPermMode(t *testing.T) {
	mux, token, _ := newAgyTestServer(t, store.ProviderAntigravityCLI)
	w := agyDo(mux, token, http.MethodPost, "/v1/providers", `{"name":"agy3","provider_type":"antigravity_cli","settings":{"perm_mode":"deny_all"},"enabled":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "perm_mode") {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}
