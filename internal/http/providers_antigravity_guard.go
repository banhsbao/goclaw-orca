package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers/antigravity"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const agyAuthCacheTTL = 5 * time.Second

type agyAuthEntry struct {
	ok bool
	at time.Time
}

type agyAuthCache struct {
	m sync.Map
}

func (c *agyAuthCache) check(ctx context.Context, id uuid.UUID) bool {
	if v, ok := c.m.Load(id); ok {
		if e := v.(agyAuthEntry); time.Since(e.at) < agyAuthCacheTTL {
			return e.ok
		}
	}
	_, err := antigravity.Models(ctx, id)
	ok := err == nil
	c.m.Store(id, agyAuthEntry{ok: ok, at: time.Now()})
	return ok
}

func (c *agyAuthCache) forget(id uuid.UUID) { c.m.Delete(id) }

func validatePermMode(p *store.LLMProviderData) error {
	if p.ProviderType != store.ProviderACP && p.ProviderType != store.ProviderAntigravityCLI {
		return nil
	}
	if len(p.Settings) == 0 {
		return nil
	}
	var s struct {
		PermMode string `json:"perm_mode"`
	}
	if err := json.Unmarshal(p.Settings, &s); err != nil {
		return nil
	}
	switch s.PermMode {
	case "", "approve-all", "approve-reads", "deny-all":
		return nil
	}
	return fmt.Errorf("perm_mode must be one of approve-all, approve-reads, deny-all")
}

func (h *ProvidersHandler) agyProviderGone(ctx context.Context, w http.ResponseWriter, locale string, id uuid.UUID) bool {
	if _, err := h.store.GetProvider(context.WithoutCancel(ctx), id); err == nil {
		return false
	}
	h.cleanupAntigravity(ctx, id)
	writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
	return true
}
