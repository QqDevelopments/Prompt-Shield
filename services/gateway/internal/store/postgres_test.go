package store_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/migrations"
	"os"
	"testing"
	"time"
)

func TestPostgresBoundary(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	org := "fixture-" + store.Token()
	other := "fixture-" + store.Token()
	if err = migrations.Apply(ctx, pool, org); err != nil {
		t.Fatal(err)
	}
	if err = migrations.Apply(ctx, pool, other); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE organization_id=$1", org)
		_, _ = pool.Exec(context.Background(), "DELETE FROM api_keys WHERE organization_id=$1", org)
		_, _ = pool.Exec(context.Background(), "DELETE FROM organizations WHERE id=$1 OR id=$2", org, other)
	}()
	repo := &store.Postgres{Pool: pool}
	policy, err := repo.Policy(ctx, org)
	if err != nil || !policy.MaskPII {
		t.Fatal("migration default policy")
	}
	policy.BlockedKeywords = []string{"synthetic-project"}
	if err = repo.SavePolicy(ctx, org, policy); err != nil {
		t.Fatal(err)
	}
	if err = repo.SavePolicy(ctx, org, policy); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale update accepted")
	}
	key := store.Key{ID: store.Token(), Name: "fixture", Prefix: "ps_test", Hash: store.Hash(store.Token()), Active: true, CreatedAt: time.Now().UTC()}
	if err = repo.AddKey(ctx, org, key); err != nil {
		t.Fatal(err)
	}
	if err = repo.RevokeKey(ctx, other, key.ID); err == nil {
		t.Fatal("cross-organization mutation")
	}
	owner, err := repo.Authenticate(ctx, key.Hash)
	if err != nil || owner != org {
		t.Fatal("authentication")
	}
	if err = repo.AddEvent(ctx, org, store.Event{ID: store.Token(), Decision: "allowed", Reason: "allowed", EntityTypes: []string{}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.Events(ctx, other, 50, 0)
	if err != nil || len(events) != 0 {
		t.Fatal("event isolation")
	}
	events, err = repo.Events(ctx, org, 50, 0)
	if err != nil || len(events) != 1 {
		t.Fatal("event persistence")
	}
	id := events[0].ID
	if err = repo.Complete(ctx, other, id, store.Completion{Outcome: "delivered", InputTokens: 999}); err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, org, id, store.Completion{Outcome: "delivered", InputTokens: 12, OutputTokens: 4}); err != nil {
		t.Fatal(err)
	}
	events, err = repo.Events(ctx, org, 50, 0, "allowed")
	if err != nil || len(events) != 1 || events[0].InputTokens != 12 || events[0].OutputTokens != 4 {
		t.Fatal("usage persistence")
	}
	filtered, err := repo.Events(ctx, org, 50, 0, "blocked")
	if err != nil || len(filtered) != 0 {
		t.Fatal("decision filter")
	}
	old := store.Event{ID: store.Token(), Decision: "masked", Reason: "masked", EntityTypes: []string{"EMAIL"}, CreatedAt: time.Now().UTC().AddDate(0, 0, -40)}
	if err = repo.AddEvent(ctx, org, old); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Prune(ctx, time.Now().UTC().AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	events, err = repo.Events(ctx, org, 50, 0)
	if err != nil || len(events) != 1 || events[0].ID != id {
		t.Fatal("retention removed wrong event")
	}
	if err = repo.RevokeKey(ctx, org, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Authenticate(ctx, key.Hash); err == nil {
		t.Fatal("revocation")
	}
}
