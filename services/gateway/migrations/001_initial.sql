CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY);
CREATE TABLE IF NOT EXISTS organizations (
 id text PRIMARY KEY,
 policy jsonb NOT NULL DEFAULT '{"mask_pii":true,"block_secrets":true,"contextual_pii":false,"blocked_keywords":[]}'
);
CREATE TABLE IF NOT EXISTS api_keys (
 id text PRIMARY KEY,
 organization_id text NOT NULL REFERENCES organizations(id),
 name text NOT NULL,
 prefix text NOT NULL,
 key_hash text NOT NULL UNIQUE,
 active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_events (
 id text PRIMARY KEY,
 organization_id text NOT NULL REFERENCES organizations(id),
 decision text NOT NULL CHECK (decision IN ('allowed','masked','blocked','failed')),
 reason text NOT NULL,
 entity_count integer NOT NULL DEFAULT 0,
 entity_types text[] NOT NULL DEFAULT '{}',
 latency_ms bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_org_created ON audit_events(organization_id,created_at DESC,id DESC);
INSERT INTO schema_migrations(version) VALUES(1) ON CONFLICT DO NOTHING;
