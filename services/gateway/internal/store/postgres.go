package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ Pool *pgxpool.Pool }

func (p *Postgres) Ready(ctx context.Context) error { return p.Pool.Ping(ctx) }
func (p *Postgres) Policy(ctx context.Context, org string) (Policy, error) {
	var data []byte
	err := p.Pool.QueryRow(ctx, "SELECT policy FROM organizations WHERE id=$1", org).Scan(&data)
	if err != nil {
		return Policy{}, err
	}
	var policy Policy
	err = json.Unmarshal(data, &policy)
	if policy.Version == 0 {
		policy.Version = 1
	}
	return policy, err
}
func (p *Postgres) SavePolicy(ctx context.Context, org string, policy Policy) error {
	previous := policy.Version
	policy.Version++
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	tag, err := p.Pool.Exec(ctx, "UPDATE organizations SET policy=$2 WHERE id=$1 AND COALESCE((policy->>'version')::integer,1)=$3", org, data, previous)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}
func (p *Postgres) Keys(ctx context.Context, org string) ([]Key, error) {
	rows, err := p.Pool.Query(ctx, "SELECT id,name,prefix,active,created_at FROM api_keys WHERE organization_id=$1 ORDER BY created_at DESC", org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []Key{}
	for rows.Next() {
		var key Key
		if err = rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.Active, &key.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
func (p *Postgres) AddKey(ctx context.Context, org string, k Key) error {
	_, err := p.Pool.Exec(ctx, "INSERT INTO api_keys(id,organization_id,name,prefix,key_hash,active,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", k.ID, org, k.Name, k.Prefix, k.Hash, k.Active, k.CreatedAt)
	return err
}
func (p *Postgres) RevokeKey(ctx context.Context, org, id string) error {
	tag, err := p.Pool.Exec(ctx, "UPDATE api_keys SET active=false WHERE organization_id=$1 AND id=$2", org, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (p *Postgres) Authenticate(ctx context.Context, hash string) (string, error) {
	var org string
	err := p.Pool.QueryRow(ctx, "SELECT organization_id FROM api_keys WHERE key_hash=$1 AND active=true", hash).Scan(&org)
	return org, err
}
func (p *Postgres) AddEvent(ctx context.Context, org string, e Event) error {
	_, err := p.Pool.Exec(ctx, "INSERT INTO audit_events(id,organization_id,decision,reason,entity_count,entity_types,latency_ms,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", e.ID, org, e.Decision, e.Reason, e.EntityCount, e.EntityTypes, e.LatencyMS, e.CreatedAt)
	return err
}
func (p *Postgres) Events(ctx context.Context, org string, limit, offset int, decision ...string) ([]Event, error) {
	filter := ""
	if len(decision) > 0 {
		filter = decision[0]
	}
	rows, err := p.Pool.Query(ctx, "SELECT id,decision,reason,entity_count,entity_types,latency_ms,created_at,outcome,input_tokens,output_tokens FROM audit_events WHERE organization_id=$1 AND ($4='' OR decision=$4) ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", org, limit, offset, filter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.Decision, &e.Reason, &e.EntityCount, &e.EntityTypes, &e.LatencyMS, &e.CreatedAt, &e.Outcome, &e.InputTokens, &e.OutputTokens); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
