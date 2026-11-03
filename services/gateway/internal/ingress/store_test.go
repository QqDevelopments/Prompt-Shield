package ingress

import (
	"context"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisAdmissionConcurrencyExpiryAndReconnect(t *testing.T) {
	address := os.Getenv("TEST_REDIS_URL")
	if address == "" {
		t.Skip("set TEST_REDIS_URL for real Redis admission verification")
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "ps:ingress:fixture:" + time.Now().UTC().Format("20060102150405.000000000")
	defer client.Del(context.Background(), key)
	reset := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	store := RedisCounter{Client: client}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			used, err := store.Admit(ctx, key, reset, 5)
			if err != nil {
				t.Error(err)
			} else if used <= 5 {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 5 {
		t.Fatalf("admitted %d", accepted.Load())
	}
	if value := client.Get(ctx, key).Val(); value != "5" {
		t.Fatalf("denied traffic changed stored count: %s", value)
	}
	ttl, err := client.ExpireTime(ctx, key).Result()
	if err != nil || int64(ttl.Seconds()) != reset.Unix() {
		t.Fatalf("reset expiry: %v %v", ttl, err)
	}
	reconnected := redis.NewClient(options)
	defer reconnected.Close()
	used, err := (RedisCounter{Client: reconnected}).Admit(ctx, key, reset, 5)
	if err != nil || used <= 5 {
		t.Fatal("reconnection lost quota")
	}
	if err := client.Set(ctx, key, "5", time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	used, err = store.Admit(ctx, key, reset, 5)
	if err != nil || used != 1 {
		t.Fatal("expired UTC bucket did not reset")
	}
}

func TestIngressConfigurationRejectsUnsafeValues(t *testing.T) {
	// Use an HTTP boundary fixture to obtain a valid baseline configuration.
	h := testIngress(t, "community", &memoryCounter{}, nil, http.NotFoundHandler())
	for _, change := range []func(*Config){
		func(c *Config) { c.Origin = "https://web.example.test/" },
		func(c *Config) { c.HMACKey = "short" },
		func(c *Config) { c.MetricsToken = "short" },
		func(c *Config) { c.Product = "unknown" },
		func(c *Config) { c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")} },
	} {
		cfg := h.cfg
		change(&cfg)
		if _, err := New(cfg, &memoryCounter{}); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}
