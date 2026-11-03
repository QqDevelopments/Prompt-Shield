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

func TestRedisAtomicLimit(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("set TEST_REDIS_URL for Redis integration")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	limiter := &Redis{Client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "fixture-" + store.Token()
	defer client.Del(context.Background(), "ps:limit:"+key)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := limiter.Allow(ctx, key, 10)
			if err != nil {
				t.Error(err)
			}
			if ok {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 10 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	ttl := client.TTL(ctx, "ps:limit:"+key).Val()
	if ttl <= 0 || ttl > time.Minute {
		t.Fatal("missing expiry")
	}
}
