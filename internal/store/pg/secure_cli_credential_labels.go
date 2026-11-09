package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var _ store.SecureCLILabeledCredentialStore = (*PGSecureCLIStore)(nil)

func (s *PGSecureCLIStore) ListCredentialLabels(ctx context.Context, q store.SecureCLICredentialLabelQuery) ([]store.SecureCLICredentialLabel, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}
	if q.UserID == "" && q.AgentID == nil {
		return nil, nil
	}

	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	var branches []string
	if q.UserID != "" {
		where := "user_id = " + next(q.UserID)
		if !isCross {
			where += " AND tenant_id = " + next(tid)
		}
		if q.BinaryID != nil {
			where += " AND binary_id = " + next(*q.BinaryID)
		}
		branches = append(branches, `SELECT binary_id, label::text AS label, description::text AS description, 'user'::text AS source FROM secure_cli_user_credentials WHERE `+where)
	}
	if q.AgentID != nil {
		where := "agent_id = " + next(*q.AgentID)
		if !isCross {
			where += " AND tenant_id = " + next(tid)
		}
		if q.BinaryID != nil {
			where += " AND binary_id = " + next(*q.BinaryID)
		}
		branches = append(branches, `SELECT binary_id, label::text AS label, description::text AS description, 'agent'::text AS source FROM secure_cli_agent_credentials WHERE `+where)
	}

	branches = append(branches, orcaServerLabelBranch(q, isCross, tid, next))
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

func (s *PGSecureCLIStore) ResolveLabeledCredential(ctx context.Context, binaryID uuid.UUID, agentID *uuid.UUID, userID, label string) (*store.SecureCLIResolvedCredential, error) {
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
		query := `SELECT encrypted_env, credential_type, host_scope FROM secure_cli_user_credentials WHERE binary_id = $1 AND user_id = $2 AND label = $3`
		args := []any{binaryID, userID, label}
		if !isCross {
			query += ` AND tenant_id = $4`
			args = append(args, tid)
		}
		res, err := s.scanResolved(ctx, query+` LIMIT 1`, args, label, "user", userID)
		if err != nil || res != nil {
			return res, err
		}
	}
	if agentID != nil {
		query := `SELECT encrypted_env, credential_type, host_scope FROM secure_cli_agent_credentials WHERE binary_id = $1 AND agent_id = $2 AND label = $3`
		args := []any{binaryID, *agentID, label}
		if !isCross {
			query += ` AND tenant_id = $4`
			args = append(args, tid)
		}
		res, err := s.scanResolved(ctx, query+` LIMIT 1`, args, label, "agent", "")
		if err != nil || res != nil {
			return res, err
		}
	}
	return s.resolveOrcaServerCredential(ctx, binaryID, label, isCross, tid)
}

func (s *PGSecureCLIStore) scanResolved(ctx context.Context, query string, args []any, label, source, subjectID string) (*store.SecureCLIResolvedCredential, error) {
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

func (s *PGSecureCLIStore) GetUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) (*store.SecureCLIUserCredential, error) {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return nil, err
	}
	return s.getUserCredential(ctx, binaryID, userID, label)
}

func (s *PGSecureCLIStore) SetUserCredentialLabeled(ctx context.Context, binaryID uuid.UUID, userID string, w store.SecureCLICredentialWrite) error {
	label, err := store.PrepareSecureCLICredentialLabel(w.Label)
	if err != nil {
		return err
	}
	desc := store.SanitizeSecureCLICredentialDescription(w.Description)
	return s.setUserCredential(ctx, binaryID, userID, label, &desc, w.Env, w.CredentialType, w.HostScope)
}

func (s *PGSecureCLIStore) DeleteUserCredentialByLabel(ctx context.Context, binaryID uuid.UUID, userID, label string) error {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return err
	}
	return s.deleteUserCredential(ctx, binaryID, userID, label)
}

func (s *PGSecureCLIStore) GetAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) (*store.SecureCLIAgentCredential, error) {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return nil, err
	}
	return s.getAgentCredential(ctx, binaryID, agentID, label)
}

func (s *PGSecureCLIStore) SetAgentCredentialLabeled(ctx context.Context, binaryID, agentID uuid.UUID, w store.SecureCLICredentialWrite, createdBy string) error {
	label, err := store.PrepareSecureCLICredentialLabel(w.Label)
	if err != nil {
		return err
	}
	desc := store.SanitizeSecureCLICredentialDescription(w.Description)
	return s.setAgentCredential(ctx, binaryID, agentID, label, &desc, w.Env, w.CredentialType, w.HostScope, createdBy)
}

func (s *PGSecureCLIStore) DeleteAgentCredentialByLabel(ctx context.Context, binaryID, agentID uuid.UUID, label string) error {
	label, err := store.PrepareSecureCLICredentialLabel(label)
	if err != nil {
		return err
	}
	return s.deleteAgentCredential(ctx, binaryID, agentID, label)
}
