ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS outcome text NOT NULL DEFAULT 'inspected';
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS input_tokens bigint NOT NULL DEFAULT 0;
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS output_tokens bigint NOT NULL DEFAULT 0;
INSERT INTO schema_migrations(version) VALUES(2) ON CONFLICT DO NOTHING;
