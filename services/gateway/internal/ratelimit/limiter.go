package ratelimit

import (
	"context"
	"github.com/redis/go-redis/v9"
	"sync"
	"time"
)

type Limiter interface {
	Allow(context.Context, string, int) (bool, error)
	Ready(context.Context) error
}
type entry struct {
	count   int
	expires time.Time
}
type Memory struct {
	mu      sync.Mutex
	entries map[string]entry
	leases  map[string]leaseEntry
}

func NewMemory() *Memory {
	return &Memory{entries: map[string]entry{}, leases: map[string]leaseEntry{}}
}
func (m *Memory) Allow(_ context.Context, key string, limit int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.entries {
		if now.After(v.expires) {
			delete(m.entries, k)
		}
	}
	v := m.entries[key]
	if v.expires.IsZero() {
		v.expires = now.Add(time.Minute)
	}
	v.count++
	m.entries[key] = v
	return v.count <= limit, nil
}
func (m *Memory) Ready(context.Context) error { return nil }

type Redis struct{ Client *redis.Client }

var increment = redis.NewScript(`local n = redis.call('INCR', KEYS[1]); if n == 1 then redis.call('PEXPIRE', KEYS[1], 60000) end; return n`)

func (r *Redis) Allow(ctx context.Context, key string, limit int) (bool, error) {
	n, err := increment.Run(ctx, r.Client, []string{"ps:limit:" + key}).Int64()
	return n <= int64(limit), err
}
func (r *Redis) Ready(ctx context.Context) error { return r.Client.Ping(ctx).Err() }
