package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	serverSelectorFlag      = "--server"
	serverLabelEchoMaxRunes = 64
)

type serverSelection int

const (
	serverPassthrough serverSelection = iota
	serverUseDefault
	serverPick
	serverAmbiguous
	serverUnknown
)

func parseServerSelector(args []string) (label string, rest []string, found bool) {
	if len(args) == 0 {
		return "", args, false
	}
	first := args[0]
	if first == serverSelectorFlag {
		if len(args) < 2 {
			return "", args[1:], true
		}
		return args[1], args[2:], true
	}
	if after, ok := strings.CutPrefix(first, serverSelectorFlag+"="); ok {
		return after, args[1:], true
	}
	return "", args, false
}

func visibleServerLabels(labels []store.SecureCLICredentialLabel) []store.SecureCLICredentialLabel {
	out := make([]store.SecureCLICredentialLabel, 0, len(labels))
	for _, l := range labels {
		if l.Label != "" {
			out = append(out, l)
		}
	}
	return out
}

func decideServerSelection(labels []store.SecureCLICredentialLabel, requested string, hasSelector, hasDefault bool) (serverSelection, string) {
	visible := visibleServerLabels(labels)
	if len(visible) == 0 {
		return serverPassthrough, ""
	}
	if !hasSelector {
		if hasDefault {
			return serverPassthrough, ""
		}
		if len(visible) == 1 {
			return serverPick, visible[0].Label
		}
		return serverAmbiguous, ""
	}
	label := store.NormalizeSecureCLICredentialLabel(requested)
	if label == "" {
		if strings.EqualFold(strings.TrimSpace(requested), store.SecureCLIDefaultLabelAlias) && hasDefault {
			return serverUseDefault, ""
		}
		return serverUnknown, ""
	}
	if err := store.ValidateSecureCLICredentialLabel(label); err != nil {
		return serverUnknown, ""
	}
	for _, l := range visible {
		if l.Label == label {
			return serverPick, label
		}
	}
	return serverUnknown, ""
}

func (t *ExecTool) selectCredentialedServer(ctx context.Context, cred *store.SecureCLIBinary,
	binary string, args []string) (*store.SecureCLIBinary, []string, *Result) {

	ls, ok := t.secureCLIStore.(store.SecureCLILabeledCredentialStore)
	if !ok || cred == nil {
		return cred, args, nil
	}
	requested, rest, hasSelector := parseServerSelector(args)
	hasDefault := cred.CredentialSource != "" || hasBinaryEnv(cred)
	if !hasSelector && hasDefault {
		return cred, args, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}
	userID := store.CredentialUserIDFromContext(ctx)
	binaryID := cred.ID

	labels, err := ls.ListCredentialLabels(ctx, store.SecureCLICredentialLabelQuery{
		BinaryID: &binaryID,
		AgentID:  agentIDPtr,
		UserID:   userID,
	})
	if err != nil {
		slog.Warn("secure_cli.labels_list_failed", "binary", binary, "agent_id", agentID, "error", err)
		if hasSelector {
			return cred, args, credentialedServerLookupError(binary)
		}
		return cred, args, nil
	}

	decision, label := decideServerSelection(labels, requested, hasSelector, hasDefault)
	switch decision {
	case serverPassthrough:
		return cred, args, nil
	case serverUseDefault:
		return cred, rest, nil
	case serverAmbiguous:
		return cred, args, credentialedServerAmbiguousError(binary, labels, hasDefault)
	case serverUnknown:
		return cred, args, credentialedServerUnknownError(binary, requested, labels, hasDefault)
	}

	resolved, err := ls.ResolveLabeledCredential(ctx, binaryID, agentIDPtr, userID, label)
	if err != nil {
		slog.Warn("secure_cli.labeled_credential_resolve_failed", "binary", binary, "label", label, "agent_id", agentID, "error", err)
		return cred, args, credentialedServerLookupError(binary)
	}
	if resolved == nil {
		return cred, args, credentialedServerUnknownError(binary, label, labels, hasDefault)
	}

	selected := *cred
	selected.ApplyLabeledCredential(resolved)
	if !hasSelector {
		rest = args
	}
	slog.Info("secure_cli.credential_selected",
		"binary", binary,
		"label", label,
		"source", resolved.Source,
		"agent_id", agentID,
		"tenant_id", store.TenantIDFromContext(ctx),
	)
	return &selected, rest, nil
}

func describeServerLabels(labels []store.SecureCLICredentialLabel, hasDefault bool) string {
	parts := make([]string, 0, len(labels)+1)
	if hasDefault {
		parts = append(parts, store.SecureCLIDefaultLabelAlias)
	}
	for _, l := range visibleServerLabels(labels) {
		entry := l.Label
		if desc := sanitizeServerDescription(l.Description); desc != "" {
			entry += " — " + desc
		}
		parts = append(parts, entry)
	}
	return strings.Join(parts, "; ")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func credentialedServerAmbiguousError(binary string, labels []store.SecureCLICredentialLabel, hasDefault bool) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Multiple servers configured for %s. Re-run with --server <label> as the first argument.\nAvailable: %s",
			binary, describeServerLabels(labels, hasDefault)),
		ForUser: fmt.Sprintf("Multiple servers are configured for %q; a server label is required.", binary),
		IsError: true,
	}
}

func credentialedServerUnknownError(binary, requested string, labels []store.SecureCLICredentialLabel, hasDefault bool) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Unknown server %q for %s. Available: %s",
			truncateRunes(requested, serverLabelEchoMaxRunes), binary, describeServerLabels(labels, hasDefault)),
		ForUser: fmt.Sprintf("Unknown server for %q.", binary),
		IsError: true,
	}
}

func credentialedServerLookupError(binary string) *Result {
	return &Result{
		ForLLM:  fmt.Sprintf("[CREDENTIALED EXEC] Could not resolve the requested server credential for %s. Try again later.", binary),
		ForUser: fmt.Sprintf("Could not resolve the server credential for %q.", binary),
		IsError: true,
	}
}

func hasBinaryEnv(cred *store.SecureCLIBinary) bool {
	if len(cred.EncryptedEnv) == 0 {
		return false
	}
	env, err := store.FlattenSecureCLIEnv(cred.EncryptedEnv)
	return err == nil && len(env) > 0
}
