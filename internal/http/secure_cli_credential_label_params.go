package http

import (
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxSecureCLICredentialLabels = 20
	maxReflectedLabelRunes       = 64
)

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func parseCredentialLabelParam(w http.ResponseWriter, r *http.Request, locale string) (string, bool) {
	raw := r.URL.Query().Get("label")
	label := store.NormalizeSecureCLICredentialLabel(raw)
	if label == "" {
		return "", true
	}
	if err := store.ValidateSecureCLICredentialLabel(label); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgSecureCLILabelInvalid, truncateRunes(label, maxReflectedLabelRunes)),
		})
		return "", false
	}
	return label, true
}

func validateCredentialDescription(w http.ResponseWriter, locale string, desc *string) (*string, bool) {
	if desc == nil {
		return nil, true
	}
	if len([]rune(*desc)) > store.SecureCLICredentialDescriptionMaxLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgSecureCLIDescriptionTooLong, store.SecureCLICredentialDescriptionMaxLen),
		})
		return nil, false
	}
	clean := store.SanitizeSecureCLICredentialDescription(*desc)
	return &clean, true
}

func writeCredentialLabelLimit(w http.ResponseWriter, locale string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error": i18n.T(locale, i18n.MsgSecureCLILabelLimit, maxSecureCLICredentialLabels),
	})
}

func (h *SecureCLIHandler) requireLabeledCredentialStore(w http.ResponseWriter, locale string) (store.SecureCLILabeledCredentialStore, bool) {
	ls, ok := h.store.(store.SecureCLILabeledCredentialStore)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "labeled credentials store unavailable")})
		return nil, false
	}
	return ls, true
}

func credentialAuditSuffix(label string) string {
	if label == "" {
		return ""
	}
	return "@" + label
}
