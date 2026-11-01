package http

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *SecureCLIHandler) handleListUserCredentials(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenants) {
		return
	}
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	creds, err := h.store.ListUserCredentials(r.Context(), binaryID)
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	// Return without env values for listing (names only + timestamps).
	// credential_type + host_scope surface so the table can show a Type badge
	// (PAT / SSH / Env) without a second round-trip per row.
	type entry struct {
		ID             uuid.UUID                                  `json:"id"`
		BinaryID       uuid.UUID                                  `json:"binary_id"`
		UserID         string                                     `json:"user_id"`
		HasEnv         bool                                       `json:"has_env"`
		EnvKeys        []string                                   `json:"env_keys,omitempty"`
		Env            map[string]store.SecureCLIEnvResponseEntry `json:"env,omitempty"`
		CredentialType *string                                    `json:"credential_type,omitempty"`
		HostScope      *string                                    `json:"host_scope,omitempty"`
		Label          string                                     `json:"label"`
		Description    string                                     `json:"description"`
		CreatedAt      string                                     `json:"created_at"`
		UpdatedAt      string                                     `json:"updated_at"`
	}
	entries := make([]entry, 0, len(creds))
	for _, c := range creds {
		envKeys := envKeysFromDecryptedJSON(c.EncryptedEnv)
		// Typed credentials (pat / ssh_key) hold a single secret in the blob.
		// Suppress env / env_keys for them — those keys would leak the wire
		// shape (`token` / `key`) to the listing endpoint.
		isTyped := c.CredentialType != nil && *c.CredentialType != "" && *c.CredentialType != "env"
		e := entry{
			ID:             c.ID,
			BinaryID:       c.BinaryID,
			UserID:         c.UserID,
			HasEnv:         len(c.EncryptedEnv) > 0,
			CredentialType: c.CredentialType,
			HostScope:      c.HostScope,
			Label:          c.Label,
			Description:    c.Description,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		}
		if !isTyped {
			e.EnvKeys = envKeys
			e.Env = store.SanitizeSecureCLIEnvJSON(c.EncryptedEnv)
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].UserID != entries[j].UserID {
			return entries[i].UserID < entries[j].UserID
		}
		return entries[i].Label < entries[j].Label
	})
	writeJSON(w, http.StatusOK, map[string]any{"user_credentials": entries})
}

func (h *SecureCLIHandler) handleGetUserCredentials(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenants) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}
	label, ok := parseCredentialLabelParam(w, r, locale)
	if !ok {
		return
	}

	var cred *store.SecureCLIUserCredential
	if label == "" {
		cred, err = h.store.GetUserCredentials(r.Context(), binaryID, userID)
	} else {
		ls, ok := h.requireLabeledCredentialStore(w, locale)
		if !ok {
			return
		}
		cred, err = ls.GetUserCredentialByLabel(r.Context(), binaryID, userID, label)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if cred == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	isTyped := cred.CredentialType != nil && *cred.CredentialType != "" && *cred.CredentialType != "env"
	resp := map[string]any{
		"user_id":         cred.UserID,
		"label":           cred.Label,
		"description":     cred.Description,
		"credential_type": cred.CredentialType,
		"host_scope":      cred.HostScope,
		"has_secret":      len(cred.EncryptedEnv) > 0,
	}
	if !isTyped {
		resp["env"] = store.SanitizeSecureCLIEnvJSON(cred.EncryptedEnv)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SecureCLIHandler) handleSetUserCredentials(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenants) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}
	label, ok := parseCredentialLabelParam(w, r, locale)
	if !ok {
		return
	}

	var body struct {
		Env         json.RawMessage `json:"env"`
		Description *string         `json:"description"`
		typedCredentialBody
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	description, ok := validateCredentialDescription(w, locale, body.Description)
	if !ok {
		return
	}

	envBytes, credType, hostScope, terr := prepareTypedCredentialEnv(r.Context(), locale, body.typedCredentialBody)
	if terr != nil {
		writeTypedCredentialError(w, terr)
		return
	}

	labeled := label != "" || description != nil
	var ls store.SecureCLILabeledCredentialStore
	var existing *store.SecureCLIUserCredential
	if labeled {
		ls, ok = h.requireLabeledCredentialStore(w, locale)
		if !ok {
			return
		}
		existing, err = h.getUserCredentialForWrite(r, ls, binaryID, userID, label)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
		if label != "" && existing == nil {
			atCap, cerr := h.userLabelLimitReached(r, binaryID, userID)
			if cerr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, cerr.Error())})
				return
			}
			if atCap {
				writeCredentialLabelLimit(w, locale)
				return
			}
		}
	}

	write := store.SecureCLICredentialWrite{Label: label}
	if description != nil {
		write.Description = *description
	} else if existing != nil {
		write.Description = existing.Description
	}

	if envBytes != nil {
		if labeled {
			write.Env, write.CredentialType, write.HostScope = envBytes, credType, hostScope
			err = ls.SetUserCredentialLabeled(r.Context(), binaryID, userID, write)
		} else {
			err = h.store.SetUserCredentialsTyped(r.Context(), binaryID, userID, envBytes, credType, hostScope)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
		emitAudit(h.msgBus, r, "secure_cli.user_credentials.updated", "secure_cli_user_credentials", binaryID.String()+"/"+userID+credentialAuditSuffix(label)+"#"+*credType)
		h.emitCacheInvalidate("")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if len(body.Env) == 0 {
		if !labeled || existing == nil || description == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env is required"})
			return
		}
		if len(existing.EncryptedEnv) == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "stored credential is unreadable; re-enter the secret")})
			return
		}
		write.Env, write.CredentialType, write.HostScope = existing.EncryptedEnv, existing.CredentialType, existing.HostScope
		if err := ls.SetUserCredentialLabeled(r.Context(), binaryID, userID, write); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
		emitAudit(h.msgBus, r, "secure_cli.user_credentials.updated", "secure_cli_user_credentials", binaryID.String()+"/"+userID+credentialAuditSuffix(label))
		h.emitCacheInvalidate("")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if !labeled {
		existing, err = h.store.GetUserCredentials(r.Context(), binaryID, userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
	}
	var existingEnv []byte
	if existing != nil {
		existingEnv = existing.EncryptedEnv
	}
	envJSON, err := store.MergeSecureCLIEnv(existingEnv, body.Env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, err.Error())})
		return
	}

	if labeled {
		write.Env = envJSON
		err = ls.SetUserCredentialLabeled(r.Context(), binaryID, userID, write)
	} else {
		err = h.store.SetUserCredentials(r.Context(), binaryID, userID, envJSON)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "secure_cli.user_credentials.updated", "secure_cli_user_credentials", binaryID.String()+"/"+userID+credentialAuditSuffix(label))
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIHandler) getUserCredentialForWrite(r *http.Request, ls store.SecureCLILabeledCredentialStore, binaryID uuid.UUID, userID, label string) (*store.SecureCLIUserCredential, error) {
	if label == "" {
		return h.store.GetUserCredentials(r.Context(), binaryID, userID)
	}
	return ls.GetUserCredentialByLabel(r.Context(), binaryID, userID, label)
}

func (h *SecureCLIHandler) userLabelLimitReached(r *http.Request, binaryID uuid.UUID, userID string) (bool, error) {
	creds, err := h.store.ListUserCredentials(r.Context(), binaryID)
	if err != nil {
		return false, err
	}
	count := 0
	for _, c := range creds {
		if c.UserID == userID && c.Label != "" {
			count++
		}
	}
	return count >= maxSecureCLICredentialLabels, nil
}

func (h *SecureCLIHandler) handleDeleteUserCredentials(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenants) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}
	label, ok := parseCredentialLabelParam(w, r, locale)
	if !ok {
		return
	}

	if label == "" {
		err = h.store.DeleteUserCredentials(r.Context(), binaryID, userID)
	} else {
		ls, ok := h.requireLabeledCredentialStore(w, locale)
		if !ok {
			return
		}
		err = ls.DeleteUserCredentialByLabel(r.Context(), binaryID, userID, label)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "secure_cli.user_credentials.deleted", "secure_cli_user_credentials", binaryID.String()+"/"+userID+credentialAuditSuffix(label))
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
