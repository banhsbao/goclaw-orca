//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteOrcaServerStore) UpdateHealth(ctx context.Context, id uuid.UUID, h store.OrcaServerHealth) error {
	checked := h.CheckedAt.UTC().Format(orcaTimeLayout)
	var healthy any
	if h.Healthy {
		healthy = checked
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE orca_servers SET last_status = ?, last_runtime_state = ?, last_error = ?,
		 last_checked_at = ?, last_healthy_at = COALESCE(?, last_healthy_at) WHERE id = ?`,
		h.Status, h.RuntimeState, h.Error, checked, healthy, id.String())
	if err != nil {
		return fmt.Errorf("update orca server health: %w", err)
	}
	return nil
}

func (s *SQLiteOrcaServerStore) ListDueForCheck(ctx context.Context, olderThan time.Time, limit int) ([]store.OrcaServer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers
		 WHERE last_checked_at IS NULL OR last_checked_at < ?
		 ORDER BY last_checked_at IS NOT NULL, last_checked_at LIMIT ?`, olderThan.UTC().Format(orcaTimeLayout), limit)
	if err != nil {
		return nil, fmt.Errorf("list due orca servers: %w", err)
	}
	defer rows.Close()
	var out []store.OrcaServer
	for rows.Next() {
		o, err := s.scan(rows, true)
		if o == nil {
			return nil, fmt.Errorf("scan due orca server: %w", err)
		}
		if err != nil {
			slog.Warn("orca_servers: skipping row with undecryptable pairing code", "id", o.ID, "error", err)
			continue
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}
