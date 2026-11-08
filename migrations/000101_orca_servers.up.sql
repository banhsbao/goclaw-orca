CREATE TABLE IF NOT EXISTS orca_servers (
    id                 UUID PRIMARY KEY,
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    label              VARCHAR(32) NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    pairing_code       BYTEA NOT NULL,
    last_status        VARCHAR(16) NOT NULL DEFAULT 'unknown',
    last_runtime_state VARCHAR(64) NOT NULL DEFAULT '',
    last_error         TEXT NOT NULL DEFAULT '',
    last_checked_at    TIMESTAMPTZ,
    last_healthy_at    TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (tenant_id, label)
);

CREATE INDEX IF NOT EXISTS idx_orca_servers_checked ON orca_servers (last_checked_at NULLS FIRST);
