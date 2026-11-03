package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/migrations"
	"os"
	"time"
)

func run() error {
	action := flag.String("action", "", "create-key, revoke-key, policy, or prune")
	org := flag.String("org", "community", "organization identifier")
	name := flag.String("name", "CLI application", "API key label")
	id := flag.String("id", "", "API key identifier")
	days := flag.Int("retention-days", 30, "audit retention in days")
	flag.Parse()
	if *action == "" {
		flag.Usage()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database_configuration_invalid")
	}
	defer pool.Close()
	if migrations.Apply(ctx, pool, *org) != nil {
		return errors.New("database_setup_failed")
	}
	repo := &store.Postgres{Pool: pool}
	switch *action {
	case "create-key":
		if len(*name) > 80 {
			return errors.New("invalid_name")
		}
		secret := "ps_" + store.Token()
		key := store.Key{ID: store.Token(), Name: *name, Prefix: secret[:12], Hash: store.Hash(secret), Active: true, CreatedAt: time.Now().UTC()}
		if repo.AddKey(ctx, *org, key) != nil {
			return errors.New("key_creation_failed")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"key": key, "secret": secret})
	case "revoke-key":
		if repo.RevokeKey(ctx, *org, *id) != nil {
			return errors.New("key_revocation_failed")
		}
		fmt.Println("Key revoked")
	case "policy":
		policy, err := repo.Policy(ctx, *org)
		if err != nil {
			return errors.New("policy_unavailable")
		}
		return json.NewEncoder(os.Stdout).Encode(policy)
	case "prune":
		if *days < 1 || *days > 3650 {
			return errors.New("invalid_retention")
		}
		count, err := repo.Prune(ctx, time.Now().UTC().AddDate(0, 0, -*days))
		if err != nil {
			return errors.New("prune_failed")
		}
		fmt.Printf("Removed %d expired audit events\n", count)
	default:
		return errors.New("unknown_action")
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
