package ratelimit

import (
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

// Both keys share a Redis cluster hash slot. Expiring leases recover after a crash.
var acquire = redis.NewScript(`
local now = redis.call('TIME')
local ms = now[1] * 1000 + math.floor(now[2] / 1000)
for i=1,2 do redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', ms) end
if (tonumber(ARGV[2])>0 and redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2])) or redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[3]) then return 0 end
for i=1,2 do
 redis.call('ZADD', KEYS[i], ms + tonumber(ARGV[4]), ARGV[1])
 redis.call('PEXPIRE', KEYS[i], ARGV[4])
end
return 1`)
var releaseLease = redis.NewScript(`for i=1,2 do redis.call('ZREM', KEYS[i], ARGV[1]) end; return 1`)

func leaseKeys(id string) []string { return []string{"ps:{public}:ip:" + id, "ps:{public}:global"} }
func (r *Redis) Acquire(ctx context.Context, id, lease string, perIP, global int, ttl time.Duration) (bool, error) {
	n, err := acquire.Run(ctx, r.Client, leaseKeys(id), lease, perIP, global, ttl.Milliseconds()).Int()
	return n == 1, err
}
func (r *Redis) Release(ctx context.Context, id, lease string) error {
	return releaseLease.Run(ctx, r.Client, leaseKeys(id), lease).Err()
}
func (m *Memory) Acquire(_ context.Context, id, lease string, perIP, global int, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	count, own := 0, 0
	for k, v := range m.leases {
		if !now.Before(v.expires) {
			delete(m.leases, k)
			continue
		}
		count++
		if v.id == id {
			own++
		}
	}
	if count >= global || (perIP > 0 && own >= perIP) {
		return false, nil
	}
	m.leases[lease] = leaseEntry{id: id, expires: now.Add(ttl)}
	return true, nil
}
func (m *Memory) Release(_ context.Context, id, lease string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases[lease].id == id {
		delete(m.leases, lease)
	}
	return nil
}

type leaseEntry struct {
	id      string
	expires time.Time
}
