package pg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGOrcaServerStore) UpdateHealth(ctx context.Context, id uuid.UUID, h store.OrcaServerHealth) error {
	checked := h.CheckedAt.UTC()
	var healthy *time.Time
	if h.Healthy {
		healthy = &checked
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE orca_servers SET last_status = $1, last_runtime_state = $2, last_error = $3,
		 last_checked_at = $4, last_healthy_at = COALESCE($5, last_healthy_at) WHERE id = $6`,
		h.Status, h.RuntimeState, h.Error, checked, healthy, id)
	if err != nil {
		return fmt.Errorf("update orca server health: %w", err)
	}
	return nil
}

func (s *PGOrcaServerStore) ListDueForCheck(ctx context.Context, olderThan time.Time, limit int) ([]store.OrcaServer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+orcaServerCols+` FROM orca_servers
		 WHERE last_checked_at IS NULL OR last_checked_at < $1
		 ORDER BY last_checked_at NULLS FIRST LIMIT $2`, olderThan.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list due orca servers: %w", err)
	}
	defer rows.Close()
	var out []store.OrcaServer
	for rows.Next() {
		o, _, err := s.scan(rows, true)
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
