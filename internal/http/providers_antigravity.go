package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers/antigravity"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *ProvidersHandler) registerAntigravityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/providers/{id}/antigravity/login/start", h.auth(h.handleAntigravityLoginStart))
	mux.HandleFunc("POST /v1/providers/{id}/antigravity/login/code", h.auth(h.handleAntigravityLoginCode))
	mux.HandleFunc("GET /v1/providers/{id}/antigravity/auth-status", h.readAuth(h.handleAntigravityAuthStatus))
}

func (h *ProvidersHandler) loadAntigravityProvider(w http.ResponseWriter, r *http.Request) (*store.LLMProviderData, bool) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return nil, false
	}
	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return nil, false
	}
	if p.ProviderType != store.ProviderAntigravityCLI {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "provider is not antigravity_cli")})
		return nil, false
	}
	return p, true
}

func writeAntigravityError(w http.ResponseWriter, locale string, err error) {
	status, key := http.StatusInternalServerError, ""
	switch {
	case errors.Is(err, antigravity.ErrNotConfigured):
		status, key = http.StatusServiceUnavailable, i18n.MsgAgyNotConfigured
	case errors.Is(err, antigravity.ErrNoLoginSession):
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAgyNoLoginSession)})
		return
	case errors.Is(err, antigravity.ErrInvalidCode):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgAgyInvalidCode)})
		return
	case errors.Is(err, antigravity.ErrLoginFailed):
		status, key = http.StatusBadGateway, i18n.MsgAgyLoginFailed
	default:
		slog.Error("providers.antigravity", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "antigravity")})
		return
	}
	slog.Warn("providers.antigravity", "error", err)
	detail := antigravity.ErrNotConfigured.Error()
	if key == i18n.MsgAgyLoginFailed {
		detail = antigravity.ErrLoginFailed.Error()
	}
	writeJSON(w, status, map[string]string{"error": i18n.T(locale, key, detail)})
}

func (h *ProvidersHandler) handleAntigravityLoginStart(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	p, ok := h.loadAntigravityProvider(w, r)
	if !ok {
		return
	}
	if err := antigravity.EnsureContainer(r.Context(), p.ID, config.ResolvedDataDirFromEnv()); err != nil {
		if errors.Is(err, antigravity.ErrPreparing) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "preparing"})
			return
		}
		writeAntigravityError(w, locale, err)
		return
	}
	if h.agyProviderGone(r.Context(), w, locale, p.ID) {
		return
	}
	authURL, already, err := h.agyLogin.Start(r.Context(), p.ID)
	if h.agyProviderGone(r.Context(), w, locale, p.ID) {
		return
	}
	if err != nil {
		writeAntigravityError(w, locale, err)
		return
	}
	if already {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_authenticated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending", "auth_url": authURL})
}

func (h *ProvidersHandler) handleAntigravityLoginCode(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	p, ok := h.loadAntigravityProvider(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	authenticated, err := h.agyLogin.SubmitCode(r.Context(), p.ID, req.Code)
	if err != nil {
		writeAntigravityError(w, locale, err)
		return
	}
	if !authenticated {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}
	h.agyAuth.forget(p.ID)
	h.emitProviderCacheInvalidate(r.Context(), p.TenantID, p.Name)
	emitAudit(h.msgBus, r, "provider.antigravity_login", "provider", p.ID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (h *ProvidersHandler) handleAntigravityAuthStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadAntigravityProvider(w, r)
	if !ok {
		return
	}
	authenticated := h.agyAuth.check(r.Context(), p.ID)
	if authenticated {
		h.agyLogin.Cancel(p.ID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": authenticated})
}

func antigravityModelInfos(ctx context.Context, id uuid.UUID) []ModelInfo {
	models, err := antigravity.Models(ctx, id)
	if err != nil {
		slog.Warn("providers.models.antigravity", "provider", id, "error", err)
		return []ModelInfo{}
	}
	out := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		out = append(out, ModelInfo{ID: m.ID, Name: m.Name})
	}
	return out
}

func (h *ProvidersHandler) cleanupAntigravity(ctx context.Context, id uuid.UUID) {
	h.agyLogin.Cancel(id)
	h.agyAuth.forget(id)
	rmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := antigravity.Remove(rmCtx, id); err != nil {
		slog.Warn("providers.antigravity.remove", "provider", id, "error", err)
	}
}
