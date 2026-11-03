package workqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client  *redis.Client
	keys    []string
	channel string
}

func NewRedis(client *redis.Client, namespace string) *Redis {
	h := sha256.Sum256([]byte(namespace))
	prefix := "ps:queue:{" + hex.EncodeToString(h[:]) + "}:"
	return &Redis{client: client, keys: []string{prefix + "waiting", prefix + "owners", prefix + "running", prefix + "sequence", prefix + "config", prefix + "notify"}, channel: prefix + "notify"}
}

// One hash slot, opaque tickets only. Redis TIME avoids cross-replica clock skew.
var transition = redis.NewScript(`
local tm=redis.call('TIME');local now=tm[1]*1000+math.floor(tm[2]/1000)
local changed=false
for _,id in ipairs(redis.call('ZRANGEBYSCORE',KEYS[2],'-inf',now)) do
 redis.call('ZREM',KEYS[1],id);redis.call('ZREM',KEYS[2],id);changed=true
end
if redis.call('ZREMRANGEBYSCORE',KEYS[3],'-inf',now)>0 then changed=true end
if ARGV[1]=='remove' then
 for i=7,#ARGV do redis.call('ZREM',KEYS[1],ARGV[i]);redis.call('ZREM',KEYS[2],ARGV[i]);redis.call('ZREM',KEYS[3],ARGV[i]) end
 redis.call('PUBLISH',KEYS[6],'changed');return {0}
end
local cfg=redis.call('GET',KEYS[5])
if cfg and cfg~=ARGV[2] then return {-2} end
redis.call('SET',KEYS[5],ARGV[2],'PX',tonumber(ARGV[6])+5000)
local function promote()
 while redis.call('ZCARD',KEYS[3])<tonumber(ARGV[3]) do
  local ids=redis.call('ZRANGE',KEYS[1],0,0)
  if #ids==0 then break end
  local id=ids[1];redis.call('ZREM',KEYS[1],id);redis.call('ZREM',KEYS[2],id)
  redis.call('ZADD',KEYS[3],now+tonumber(ARGV[6]),id);changed=true
 end
end
if ARGV[1]=='sync' then
 for i=7,#ARGV do
  if redis.call('ZSCORE',KEYS[1],ARGV[i]) then redis.call('ZADD',KEYS[2],now+tonumber(ARGV[5]),ARGV[i]) end
 end
end
promote()
if ARGV[1]=='join' then
 if redis.call('ZCARD',KEYS[3])>=tonumber(ARGV[3]) and redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[4]) then return {-1} end
 local seq=redis.call('INCR',KEYS[4]);redis.call('ZADD',KEYS[1],seq,ARGV[7])
 redis.call('ZADD',KEYS[2],now+tonumber(ARGV[5]),ARGV[7]);changed=true;promote()
end
for i=1,4 do redis.call('PEXPIRE',KEYS[i],tonumber(ARGV[6])+5000) end
if changed then redis.call('PUBLISH',KEYS[6],'changed') end
local result={0}
for i=7,#ARGV do
 local expiry=redis.call('ZSCORE',KEYS[3],ARGV[i])
 if expiry then table.insert(result,0);table.insert(result,math.max(0,tonumber(expiry)-now))
 else
  local rank=redis.call('ZRANK',KEYS[1],ARGV[i])
  table.insert(result,rank and rank+1 or -3);table.insert(result,0)
 end
end
return result`)

func (r *Redis) call(ctx context.Context, op string, ids []string, s Settings) (map[string]Snapshot, error) {
	args := []any{op, s.fingerprint(), s.Running, s.Waiting, s.OwnerTTL.Milliseconds(), (s.Processing + 5*time.Second).Milliseconds()}
	for _, id := range ids {
		args = append(args, id)
	}
	values, err := transition.Run(ctx, r.client, r.keys, args...).Int64Slice()
	if err != nil || len(values) == 0 {
		return nil, ErrUnavailable
	}
	if values[0] == -1 {
		return nil, ErrFull
	}
	if values[0] == -2 {
		return nil, ErrConfig
	}
	out := map[string]Snapshot{}
	if len(values) != 1+len(ids)*2 {
		return nil, ErrUnavailable
	}
	for i, id := range ids {
		position := int(values[1+i*2])
		state := Snapshot{Position: position, Remaining: time.Duration(values[2+i*2]) * time.Millisecond}
		if position < 0 {
			state.Err = ErrUnavailable
		}
		out[id] = state
	}
	return out, nil
}
func (r *Redis) Join(ctx context.Context, id string, s Settings) (Snapshot, error) {
	out, err := r.call(ctx, "join", []string{id}, s)
	return out[id], err
}
func (r *Redis) Sync(ctx context.Context, ids []string, s Settings) (map[string]Snapshot, error) {
	return r.call(ctx, "sync", ids, s)
}
func (r *Redis) Remove(ctx context.Context, ids ...string) error {
	s := Defaults()
	args := []any{"remove", s.fingerprint(), s.Running, s.Waiting, s.OwnerTTL.Milliseconds(), (s.Processing + 5*time.Second).Milliseconds()}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := transition.Run(ctx, r.client, r.keys, args...).Int64Slice()
	return err
}
func (r *Redis) Subscribe(ctx context.Context) (<-chan struct{}, func()) {
	pub := r.client.Subscribe(ctx, r.channel)
	out := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-pub.Channel():
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out, func() { _ = pub.Close() }
}
