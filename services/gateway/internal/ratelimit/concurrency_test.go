package ratelimit

import (
	"context"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/redis/go-redis/v9"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedisPublicConcurrency(t *testing.T) {
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("Redis integration URL required")
	}
	options, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	limiter := &Redis{Client: client}
	ctx := context.Background()
	id := store.Token()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	leases := make(chan string, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease := store.Token()
			ok, err := limiter.Acquire(ctx, id, lease, 2, 20, 3*time.Second)
			if err != nil {
				t.Error(err)
			}
			if ok {
				admitted.Add(1)
				leases <- lease
			}
		}()
	}
	wg.Wait()
	close(leases)
	if admitted.Load() != 2 {
		t.Fatalf("per-IP cap: %d", admitted.Load())
	}
	for lease := range leases {
		if limiter.Release(ctx, id, lease) != nil {
			t.Fatal("release")
		}
	}
	lease := store.Token()
	if ok, err := limiter.Acquire(ctx, id, lease, 2, 20, time.Second); err != nil || !ok {
		t.Fatal("capacity not returned")
	}
	defer limiter.Release(ctx, id, lease)
	if ttl := client.PTTL(ctx, leaseKeys(id)[0]).Val(); ttl <= 0 || ttl > time.Second {
		t.Fatal("lease expiry missing")
	}
}
