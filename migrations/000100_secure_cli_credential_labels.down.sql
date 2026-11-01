DELETE FROM secure_cli_user_credentials WHERE label <> '';
DELETE FROM secure_cli_agent_credentials WHERE label <> '';

DROP INDEX IF EXISTS idx_scuc_tenant_user;

ALTER TABLE secure_cli_user_credentials DROP CONSTRAINT IF EXISTS uq_scuc_binary_user_tenant_label;
ALTER TABLE secure_cli_agent_credentials DROP CONSTRAINT IF EXISTS uq_scac_binary_agent_tenant_label;

ALTER TABLE secure_cli_user_credentials ADD UNIQUE (binary_id, user_id, tenant_id);
ALTER TABLE secure_cli_agent_credentials ADD UNIQUE (binary_id, agent_id, tenant_id);

ALTER TABLE secure_cli_user_credentials DROP COLUMN IF EXISTS description, DROP COLUMN IF EXISTS label;
ALTER TABLE secure_cli_agent_credentials DROP COLUMN IF EXISTS description, DROP COLUMN IF EXISTS label;
