package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func orcaServerLabelBranch(q store.SecureCLICredentialLabelQuery, isCross bool, tid uuid.UUID, next func(any) string) string {
	var where []string
	if !isCross {
		where = append(where, "o.tenant_id = "+next(tid))
	}
	if q.BinaryID != nil {
		where = append(where, "b.id = "+next(*q.BinaryID))
	}
	query := `SELECT b.id AS binary_id, o.label::text AS label, o.description::text AS description, 'orca_server'::text AS source FROM orca_servers o JOIN secure_cli_binaries b ON b.binary_name = 'orca' AND b.tenant_id = o.tenant_id`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	return query
}

func (s *PGSecureCLIStore) resolveOrcaServerCredential(ctx context.Context, binaryID uuid.UUID, label string, isCross bool, tid uuid.UUID) (*store.SecureCLIResolvedCredential, error) {
	query := `SELECT o.id, o.pairing_code FROM orca_servers o JOIN secure_cli_binaries b ON b.id = $1 AND b.binary_name = 'orca' AND b.tenant_id = o.tenant_id WHERE o.label = $2`
	args := []any{binaryID, label}
	if !isCross {
		query += ` AND o.tenant_id = $3`
		args = append(args, tid)
	}
	var (
		id   uuid.UUID
		code []byte
	)
	err := s.db.QueryRowContext(ctx, query+` LIMIT 1`, args...).Scan(&id, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain := string(code)
	if s.encKey != "" {
		plain, err = crypto.Decrypt(plain, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt orca pairing code: %w", err)
		}
	}
	env, err := store.OrcaServerCredentialEnv(plain)
	if err != nil {
		return nil, err
	}
	return &store.SecureCLIResolvedCredential{Label: label, Source: store.SecureCLISourceOrcaServer, SubjectID: id.String(), Env: env}, nil
}
