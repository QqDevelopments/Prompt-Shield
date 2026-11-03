package ratelimit

import (
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

// Reserve conservatively charges input bytes plus maximum output tokens in a UTC day.
// Reservations are not refunded after dispatch, including upstream failures.
func (m *Memory) Reserve(_ context.Context, key string, cost, limit int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	key = "budget:" + now.Format("2006-01-02") + ":" + key
	value := m.entries[key]
	if value.count+cost > limit {
		return false, nil
	}
	value.count += cost
	value.expires = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	m.entries[key] = value
	return true, nil
}

var reserve = redis.NewScript(`local current = tonumber(redis.call('GET', KEYS[1]) or '0'); local cost = tonumber(ARGV[1]); if current + cost > tonumber(ARGV[2]) then return 0 end; redis.call('INCRBY', KEYS[1], cost); redis.call('EXPIRE', KEYS[1], ARGV[3]); return 1`)

func (r *Redis) Reserve(ctx context.Context, key string, cost, limit int) (bool, error) {
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	result, err := reserve.Run(ctx, r.Client, []string{"ps:budget:" + now.Format("2006-01-02") + ":" + key}, cost, limit, int(end.Sub(now).Seconds())+1).Int()
	return result == 1, err
}
