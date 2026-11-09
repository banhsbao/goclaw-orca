//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var _ store.SecureCLILabeledCredentialStore = (*SQLiteSecureCLIStore)(nil)

func (s *SQLiteSecureCLIStore) ListCredentialLabels(ctx context.Context, q store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}
	if q.UserID == "" && q.AgentID == nil {
		return nil, nil
	}

	var args []any
	var branches []string
	build := func(table, keyCol, source string, key any) {
		where := keyCol + " = ?"
		args = append(args, key)
		if !isCross {
			where += " AND tenant_id = ?"
			args = append(args, tid)
		}
		if q.BinaryID != nil {
			where += " AND binary_id = ?"
			args = append(args, *q.BinaryID)
		}
		branches = append(branches, `SELECT binary_id, label, description, '`+source+`' AS source FROM `+table+` WHERE `+where)
	}
	if q.UserID != "" {
		build("secure_cli_user_credentials", "user_id", "user", q.UserID)
	}
	if q.AgentID != nil {
		build("secure_cli_agent_credentials", "agent_id", "agent", *q.AgentID)
	}

	branches = append(branches, orcaServerLabelBranch(q, isCross, tid, &args))
	query := strings.Join(branches, ` UNION ALL `) + ` ORDER BY 1, 2`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []store.SecureCLICredentialLabel
	for rows.Next() {
		var l store.SecureCLICredentialLabel
		if err := rows.Scan(&l.BinaryID, &l.Label, &l.Description, &l.Source); err != nil {
			return nil, err
		}
		all = append(all, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return store.MergeSecureCLICredentialLabels(all), nil
}

func (s *SQLiteSecureCLIStore) ResolveLabeledCredential(ctx context.Context, binaryID uuid.UUID, agentID *uuid.UUID, userID, label string) (*store.SecureCLIResolvedCredential, error) {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return nil, err
	}
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	if userID != "" {
		query := `SELECT encrypted_env, credential_type, host_scope FROM secure_cli_user_credentials WHERE binary_id = ? AND user_id = ? AND label = ?`
		args := []any{binaryID, userID, label}
		if !isCross {
			query += ` AND tenant_id = ?`
			args = append(args, tid)
		}
		res, err := s.scanResolved(ctx, query+` LIMIT 1`, args, label, "user", userID)
		if err != nil || res != nil {
			return res, err
		}
	}
	if agentID != nil {
		query := `SELECT encrypted_env, credential_type, host_scope FROM secure_cli_agent_credentials WHERE binary_id = ? AND agent_id = ? AND label = ?`
		args := []any{binaryID, *agentID, label}
		if !isCross {
			query += ` AND tenant_id = ?`
			args = append(args, tid)
		}
		res, err := s.scanResolved(ctx, query+` LIMIT 1`, args, label, "agent", "")
		if err != nil || res != nil {
			return res, err
		}
	}
	return s.resolveOrcaServerCredential(ctx, binaryID, label, isCross, tid)
}

func (s *SQLiteSecureCLIStore) scanResolved(ctx context.Context, query string, args []any, label, source, subjectID string) (*store.SecureCLIResolvedCredential, error) {
	var env []byte
	res := &store.SecureCLIResolvedCredential{Label: label, Source: source, SubjectID: subjectID}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&env, &res.CredentialType, &res.HostScope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res.Env = s.decryptAgentCredentialEnv(env)
	return res, nil
}

func (s *SQLiteSecureCLIStore) GetUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) (*store.SecureCLIUserCredential, error) {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return nil, err
	}
	return s.getUserCredential(ctx, binaryID, userID, label)
}

func (s *SQLiteSecureCLIStore) SetUserCredentialLabeled(ctx context.Context, binaryID uuid.UUID, userID string, w store.SecureCLICredentialWrite) error {
	label, err := store.PrepareSecureCLICredentialLabel(w.Label)
	if err != nil {
		return err
	}
	desc := store.SanitizeSecureCLICredentialDescription(w.Description)
	return s.setUserCredential(ctx, binaryID, userID, label, &desc, w.Env, w.CredentialType, w.HostScope)
}

func (s *SQLiteSecureCLIStore) DeleteUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) error {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return err
	}
	return s.deleteUserCredential(ctx, binaryID, userID, label)
}

func (s *SQLiteSecureCLIStore) GetAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) (*store.SecureCLIAgentCredential, error) {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return nil, err
	}
	return s.getAgentCredential(ctx, binaryID, agentID, label)
}

func (s *SQLiteSecureCLIStore) SetAgentCredentialLabeled(ctx context.Context, binaryID, agentID uuid.UUID, w store.SecureCLICredentialWrite, createdBy string) error {
	label, err := store.PrepareSecureCLICredentialLabel(w.Label)
	if err != nil {
		return err
	}
	desc := store.SanitizeSecureCLICredentialDescription(w.Description)
	return s.setAgentCredential(ctx, binaryID, agentID, label, &desc, w.Env, w.CredentialType, w.HostScope, createdBy)
}

func (s *SQLiteSecureCLIStore) DeleteAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) error {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return err
	}
	return s.deleteAgentCredential(ctx, binaryID, agentID, label)
}
