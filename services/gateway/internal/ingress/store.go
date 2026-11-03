// Package ingress implements admission before the application gateway.
package ingress

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Counter is the shared, UTC-day admission boundary. A denied request must not
// increment the counter. Keys contain HMAC identifiers, never client addresses.
type Counter interface {
	Admit(context.Context, string, time.Time, int) (used int, err error)
}

type RedisCounter struct{ Client *redis.Client }

var admission = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local limit = tonumber(ARGV[1])
if current >= limit then return current + 1 end
local used = redis.call('INCR', KEYS[1])
redis.call('EXPIREAT', KEYS[1], ARGV[2])
return used
`)

func (s RedisCounter) Admit(ctx context.Context, key string, reset time.Time, limit int) (int, error) {
	return admission.Run(ctx, s.Client, []string{key}, limit, reset.Unix()).Int()
}
