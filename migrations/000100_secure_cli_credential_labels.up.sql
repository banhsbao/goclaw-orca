ALTER TABLE secure_cli_user_credentials
    ADD COLUMN label VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN description TEXT NOT NULL DEFAULT '';

ALTER TABLE secure_cli_agent_credentials
    ADD COLUMN label VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN description TEXT NOT NULL DEFAULT '';

DO $$
DECLARE c text;
BEGIN
    FOR c IN SELECT conname FROM pg_constraint
             WHERE conrelid = 'secure_cli_user_credentials'::regclass AND contype = 'u'
    LOOP
        EXECUTE format('ALTER TABLE secure_cli_user_credentials DROP CONSTRAINT %I', c);
    END LOOP;
    FOR c IN SELECT conname FROM pg_constraint
             WHERE conrelid = 'secure_cli_agent_credentials'::regclass AND contype = 'u'
    LOOP
        EXECUTE format('ALTER TABLE secure_cli_agent_credentials DROP CONSTRAINT %I', c);
    END LOOP;
END $$;

ALTER TABLE secure_cli_user_credentials
    ADD CONSTRAINT uq_scuc_binary_user_tenant_label UNIQUE (binary_id, user_id, tenant_id, label);

ALTER TABLE secure_cli_agent_credentials
    ADD CONSTRAINT uq_scac_binary_agent_tenant_label UNIQUE (binary_id, agent_id, tenant_id, label);

CREATE INDEX IF NOT EXISTS idx_scuc_tenant_user ON secure_cli_user_credentials(tenant_id, user_id);
