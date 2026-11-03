// bench-fixture runs production HTTP handlers with isolated real dependencies.
// It is a local verification executable, never included in the gateway image.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/api"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/engine"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/ratelimit"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/migrations"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	output := flag.String("output", "", "private fixture connection JSON")
	delay := flag.Duration("upstream-delay", 0, "synthetic upstream delay (after first SSE event)")
	flag.Parse()
	if *output == "" || os.Getenv("TEST_DATABASE_URL") == "" || os.Getenv("TEST_REDIS_URL") == "" {
		return errors.New("output and TEST_DATABASE_URL/TEST_REDIS_URL required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	setup, done := context.WithTimeout(ctx, 30*time.Second)
	defer done()
	pool, err := pgxpool.New(setup, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	opts, err := redis.ParseURL(os.Getenv("TEST_REDIS_URL"))
	if err != nil {
		return err
	}
	cache := redis.NewClient(opts)
	defer cache.Close()
	if err = cache.Ping(setup).Err(); err != nil {
		return err
	}
	realEngine, err := engine.New(os.Getenv("RUST_ENGINE_ADDR"), os.Getenv("ENGINE_TOKEN"))
	if err != nil {
		return err
	}
	defer realEngine.Close()
	if err = realEngine.Ready(setup); err != nil {
		return err
	}
	repo := &store.Postgres{Pool: pool}
	keys := map[string]string{}
	prefix := "bench-" + store.Token()
	for _, kind := range []string{"rules", "ner"} {
		org := prefix + "-" + kind
		if err = migrations.Apply(setup, pool, org); err != nil {
			return err
		}
		policy := store.DefaultPolicy()
		policy.ContextualPII = kind == "ner"
		if err = repo.SavePolicy(setup, org, policy); err != nil {
			return err
		}
		key := "ps_" + store.Token()
		if err = repo.AddKey(setup, org, store.Key{ID: store.Token(), Name: "synthetic-load", Prefix: "ps_", Hash: store.Hash(key), Active: true, CreatedAt: time.Now()}); err != nil {
			return err
		}
		keys[kind] = key
	}
	var active, dispatched, canceled, privacyFailures atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		dispatched.Add(1)
		var request struct {
			Messages []struct{ Content string }
			Stream   bool
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024)).Decode(&request) != nil || len(request.Messages) != 1 {
			w.WriteHeader(400)
			return
		}
		text := request.Messages[0].Content
		if strings.Contains(text, "alice@example.com") || strings.Contains(text, "John Smith") || strings.Contains(text, "sk-abcdefghijklmnopqrstuvwx") {
			privacyFailures.Add(1)
			w.WriteHeader(400)
			return
		}
		emit := func(value any) {
			data, _ := json.Marshal(value)
			fmt.Fprintf(w, "data: %s\n\n", data)
			_ = http.NewResponseController(w).Flush()
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			emit(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": text}, "finish_reason": nil}}})
		}
		select {
		case <-r.Context().Done():
			canceled.Add(1)
			return
		case <-time.After(*delay):
		}
		if request.Stream {
			emit(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}})
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}}})
		}
	}))
	defer upstream.Close()
	statsToken := store.Token()
	gateway := api.New(api.Config{Organization: prefix + "-rules", DailyTokenBudget: 1 << 50,
		OpenAIURL: upstream.URL, OpenAIKey: "synthetic-upstream-key", EngineTimeout: time.Second, UpstreamTimeout: 5 * time.Second},
		repo, &ratelimit.Redis{Client: cache}, realEngine).Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__bench/") {
			if r.Header.Get("Authorization") != "Bearer "+statsToken {
				w.WriteHeader(401)
				return
			}
			if r.URL.Path == "/__bench/heap" {
				_ = pprof.WriteHeapProfile(w)
				return
			}
			if r.URL.Path != "/__bench/stats" {
				w.WriteHeader(404)
				return
			}
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			_ = json.NewEncoder(w).Encode(map[string]any{"pid": os.Getpid(), "memory": mem, "active_upstream": active.Load(),
				"dispatched_upstream": dispatched.Load(), "canceled_upstream": canceled.Load(), "privacy_failures": privacyFailures.Load(),
				"scope": "fixture process includes production gateway handlers and synthetic upstream"})
			return
		}
		gateway.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	data, _ := json.Marshal(map[string]any{"url": "http://" + listener.Addr().String(), "keys": keys, "stats_token": statsToken, "pid": os.Getpid(), "upstream_delay_ms": delay.Milliseconds()})
	if err = os.WriteFile(*output, data, 0600); err != nil {
		return err
	}
	fmt.Println("isolated benchmark fixture ready")
	shutdown := make(chan error, 1)
	go func() { shutdown <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		finish, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(finish)
	case err = <-shutdown:
		return err
	}
}
