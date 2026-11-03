package migrations

import (
	"context"
	_ "embed"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 001_initial.sql
var initial string

//go:embed 002_usage.sql
var usage string

func Apply(ctx context.Context, pool *pgxpool.Pool, org string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(8841291)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, initial); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, usage); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO organizations(id) VALUES($1) ON CONFLICT DO NOTHING", org); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
