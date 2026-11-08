//go:build sqlite || sqliteonly

package sqlitestore

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

func orcaServerLabelBranch(q store.SecureCLICredentialLabelQuery, isCross bool, tid uuid.UUID, args *[]any) string {
	var where []string
	if !isCross {
		where = append(where, "o.tenant_id = ?")
		*args = append(*args, tid)
	}
	if q.BinaryID != nil {
		where = append(where, "b.id = ?")
		*args = append(*args, *q.BinaryID)
	}
	query := `SELECT b.id AS binary_id, o.label AS label, o.description AS description, 'orca_server' AS source FROM orca_servers o JOIN secure_cli_binaries b ON b.binary_name = 'orca' AND b.tenant_id = o.tenant_id`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	return query
}

func (s *SQLiteSecureCLIStore) resolveOrcaServerCredential(ctx context.Context, binaryID uuid.UUID, label string, isCross bool, tid uuid.UUID) (*store.SecureCLIResolvedCredential, error) {
	query := `SELECT o.id, o.pairing_code FROM orca_servers o JOIN secure_cli_binaries b ON b.id = ? AND b.binary_name = 'orca' AND b.tenant_id = o.tenant_id WHERE o.label = ?`
	args := []any{binaryID, label}
	if !isCross {
		query += ` AND o.tenant_id = ?`
		args = append(args, tid)
	}
	var (
		id   string
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
	return &store.SecureCLIResolvedCredential{Label: label, Source: store.SecureCLISourceOrcaServer, SubjectID: id, Env: env}, nil
}
