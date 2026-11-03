package workqueue

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func exercise(t *testing.T, first, second Store) {
	t.Helper()
	cfg := Defaults()
	cfg.Running = 1
	cfg.Waiting = 1
	cfg.Wait = time.Second
	a, b := New(first, cfg), New(second, cfg)
	defer a.Close()
	defer b.Close()
	ctx := context.Background()
	one, err := a.Acquire(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Release()
	waiting := make(chan struct{}, 1)
	cancelled, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		lease, e := b.Acquire(cancelled, func(position int) {
			if position != 1 {
				t.Error("wrong FIFO position")
			}
			select {
			case waiting <- struct{}{}:
			default:
			}
		})
		if lease != nil {
			lease.Release()
		}
		result <- e
	}()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("did not queue")
	}
	if _, err := a.Acquire(ctx, nil); !errors.Is(err, ErrFull) {
		t.Fatalf("full queue: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	one.Release()
	next, err := b.Acquire(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	next.Release()
	active, err := a.Acquire(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(ctx, nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	active.Release()
	next, err = b.Acquire(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	next.Release()
}

func TestMemoryQueueCancellationCapacityAndRecovery(t *testing.T) {
	store := NewMemory()
	exercise(t, store, store)
}

func redisFixture(t *testing.T) *redis.Client {
	t.Helper()
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL required for real Redis queue integration")
	}
	opt, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opt)
	t.Cleanup(func() { client.Close() })
	if err = client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRedisQueueSharedAcrossReplicas(t *testing.T) {
	client := redisFixture(t)
	namespace := "queue-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	a, b := NewRedis(client, namespace), NewRedis(client, namespace)
	t.Cleanup(func() { client.Del(context.Background(), a.keys...).Err() })
	exercise(t, a, b)
	other := NewRedis(client, namespace+"-other")
	t.Cleanup(func() { client.Del(context.Background(), other.keys...).Err() })
	if _, err := other.Join(context.Background(), "opaque-test-ticket", Defaults()); err != nil {
		t.Fatal(err)
	}
	changed := Defaults()
	changed.Running = 3
	if _, err := other.Sync(context.Background(), nil, changed); !errors.Is(err, ErrConfig) {
		t.Fatalf("mixed settings accepted: %v", err)
	}
}

func TestQueueCloseReleasesWaitingRequests(t *testing.T) {
	cfg := Defaults()
	cfg.Running = 1
	q := New(NewMemory(), cfg)
	one, err := q.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Release()
	waiting := make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() { _, e := q.Acquire(context.Background(), func(int) { waiting <- struct{}{} }); result <- e }()
	<-waiting
	q.Close()
	select {
	case err := <-result:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel waiter")
	}
	if context.Cause(one.Context) != ErrUnavailable {
		t.Fatal("active context was not cancelled")
	}
}

func TestSettingsAndSwitchValidation(t *testing.T) {
	for _, value := range []string{"true", "false", ""} {
		if _, err := Bool(func(string) string { return value }, "flag", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Bool(func(string) string { return "yes" }, "flag", true); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	for _, name := range []string{"QUEUE_MAX_RUNNING", "QUEUE_MAX_WAITING", "QUEUE_WAIT_TIMEOUT_SECONDS"} {
		if _, err := FromEnv(func(key string) string {
			if key == name {
				return "-1"
			}
			return ""
		}); err == nil {
			t.Fatal("invalid queue setting accepted")
		}
	}
}

func fifo(t *testing.T, a, b Store) {
	t.Helper()
	ctx := context.Background()
	cfg := Defaults()
	cfg.Running = 1
	for i, id := range []string{"first", "second", "third"} {
		state, err := a.Join(ctx, id, cfg)
		if err != nil || state.Position != i {
			t.Fatalf("position %d: %+v %v", i, state, err)
		}
	}
	if err := b.Remove(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	states, err := b.Sync(ctx, []string{"second", "third"}, cfg)
	if err != nil || states["second"].Position != 0 || states["third"].Position != 1 {
		t.Fatalf("FIFO: %+v %v", states, err)
	}
	if err := a.Remove(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	states, err = a.Sync(ctx, []string{"third"}, cfg)
	if err != nil || states["third"].Position != 0 {
		t.Fatalf("FIFO promotion: %+v %v", states, err)
	}
}

func TestFIFOAcrossStores(t *testing.T) {
	t.Run("memory", func(t *testing.T) { store := NewMemory(); fifo(t, store, store) })
	t.Run("redis", func(t *testing.T) {
		client := redisFixture(t)
		namespace := "fifo-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		a, b := NewRedis(client, namespace), NewRedis(client, namespace)
		t.Cleanup(func() { client.Del(context.Background(), a.keys...).Err() })
		fifo(t, a, b)
	})
}

type quietStore struct{ Store }

func (q quietStore) Subscribe(context.Context) (<-chan struct{}, func()) {
	return make(chan struct{}), func() {}
}

func TestLostNotificationReconcilesWithoutBrowserPolling(t *testing.T) {
	cfg := Defaults()
	cfg.Running = 1
	cfg.Wait = 5 * time.Second
	store := NewMemory()
	a, b := New(store, cfg), New(quietStore{store}, cfg)
	defer a.Close()
	defer b.Close()
	one, err := a.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Release()
	waiting := make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() {
		lease, e := b.Acquire(context.Background(), func(int) {
			select {
			case waiting <- struct{}{}:
			default:
			}
		})
		if lease != nil {
			lease.Release()
		}
		result <- e
	}()
	<-waiting
	one.Release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("missed pubsub notification stalled queue")
	}
}

func TestRedisOwnerExpiryAndRunningDeadline(t *testing.T) {
	client := redisFixture(t)
	store := NewRedis(client, "lease-test-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	t.Cleanup(func() { client.Del(context.Background(), store.keys...).Err() })
	cfg := Defaults()
	cfg.Running = 1
	cfg.OwnerTTL = 200 * time.Millisecond
	cfg.Processing = 50 * time.Millisecond
	ctx := context.Background()
	for _, id := range []string{"active", "dead-waiter", "live-waiter"} {
		if _, err := store.Join(ctx, id, cfg); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(125 * time.Millisecond)
	if _, err := store.Sync(ctx, []string{"live-waiter"}, cfg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(125 * time.Millisecond)
	if err := store.Remove(ctx, "active"); err != nil {
		t.Fatal(err)
	}
	states, err := store.Sync(ctx, []string{"dead-waiter", "live-waiter"}, cfg)
	if err != nil || states["dead-waiter"].Err != ErrUnavailable || states["live-waiter"].Position != 0 {
		t.Fatalf("owner cleanup: %+v %v", states, err)
	}
	// A crashed running owner cannot retain capacity past its bounded processing lease.
	time.Sleep(5100 * time.Millisecond)
	next, err := store.Join(ctx, "after-crash", cfg)
	if err != nil || next.Position != 0 {
		t.Fatalf("running lease did not expire: %+v %v", next, err)
	}
}

func TestUnavailableRedisRejectsWork(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	defer client.Close()
	q := New(NewRedis(client, "unavailable-test"), Defaults())
	defer q.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := q.Acquire(ctx, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Redis outage admitted work: %v", err)
	}
}

func TestRedisParallelAdmissionKeepsSharedLimits(t *testing.T) {
	client := redisFixture(t)
	namespace := "parallel-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	a, b := NewRedis(client, namespace), NewRedis(client, namespace)
	t.Cleanup(func() { client.Del(context.Background(), a.keys...).Err() })
	cfg := Defaults()
	cfg.Running = 2
	cfg.Waiting = 3
	result := make(chan Snapshot, 15)
	start := make(chan struct{})
	for i := range 15 {
		go func() {
			<-start
			store := a
			if i%2 == 1 {
				store = b
			}
			state, err := store.Join(context.Background(), strconv.Itoa(i), cfg)
			state.Err = err
			result <- state
		}()
	}
	close(start)
	running, waiting, full := 0, 0, 0
	for range 15 {
		state := <-result
		if errors.Is(state.Err, ErrFull) {
			full++
		} else if state.Err != nil {
			t.Fatal(state.Err)
		} else if state.Position == 0 {
			running++
		} else {
			waiting++
		}
	}
	if running != 2 || waiting != 3 || full != 10 {
		t.Fatalf("shared capacity running=%d waiting=%d full=%d", running, waiting, full)
	}
	ids := make([]string, 15)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
	}
	if err := b.Remove(context.Background(), ids...); err != nil {
		t.Fatal(err)
	}
	cleaned, err := a.Join(context.Background(), "after-batch-cleanup", cfg)
	if err != nil || cleaned.Position != 0 {
		t.Fatalf("batch cleanup retained capacity: %+v %v", cleaned, err)
	}
	other := NewRedis(client, namespace+"-isolated")
	t.Cleanup(func() { client.Del(context.Background(), other.keys...).Err() })
	state, err := other.Join(context.Background(), "isolated", cfg)
	if err != nil || state.Position != 0 {
		t.Fatalf("independent core shared capacity: %+v %v", state, err)
	}
}

type faultStore struct {
	Store
	down atomic.Bool
}

func (s *faultStore) Join(ctx context.Context, id string, cfg Settings) (Snapshot, error) {
	if s.down.Load() {
		return Snapshot{}, ErrUnavailable
	}
	return s.Store.Join(ctx, id, cfg)
}
func (s *faultStore) Sync(ctx context.Context, ids []string, cfg Settings) (map[string]Snapshot, error) {
	if s.down.Load() {
		return nil, ErrUnavailable
	}
	return s.Store.Sync(ctx, ids, cfg)
}

func (s *faultStore) Remove(ctx context.Context, ids ...string) error {
	if s.down.Load() {
		return ErrUnavailable
	}
	return s.Store.Remove(ctx, ids...)
}

func TestCoordinationFaultCancelsRunningAndWaitingAndRecovers(t *testing.T) {
	store := &faultStore{Store: NewMemory()}
	cfg := Defaults()
	cfg.Running = 1
	q := New(store, cfg)
	defer q.Close()
	active, err := q.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Release()
	waiting, result := make(chan struct{}, 1), make(chan error, 1)
	go func() {
		lease, e := q.Acquire(context.Background(), func(int) {
			select {
			case waiting <- struct{}{}:
			default:
			}
		})
		if lease != nil {
			lease.Release()
		}
		result <- e
	}()
	<-waiting
	store.down.Store(true)
	if _, err := q.Acquire(context.Background(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed coordination admitted work")
	}
	select {
	case <-active.Context.Done():
		if !errors.Is(context.Cause(active.Context), ErrUnavailable) {
			t.Fatal(context.Cause(active.Context))
		}
	case <-time.After(4 * time.Second):
		t.Fatal("lost coordination did not cancel active work")
	}
	if err := <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	active.Release()
	store.down.Store(false)
	next, err := q.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	next.Release()
	q.Close()
	if !errors.Is(q.Ready(context.Background()), ErrUnavailable) {
		t.Fatal("closed scheduler remained ready")
	}
}

type heldJoin struct {
	Store
	entered, proceed chan struct{}
}

func (s heldJoin) Join(ctx context.Context, id string, cfg Settings) (Snapshot, error) {
	close(s.entered)
	<-s.proceed
	return s.Store.Join(ctx, id, cfg)
}

func TestShutdownDuringAdmissionDoesNotStartOrRetainWork(t *testing.T) {
	store := NewMemory()
	held := heldJoin{store, make(chan struct{}), make(chan struct{})}
	proceed := sync.OnceFunc(func() { close(held.proceed) })
	defer proceed()
	cfg := Defaults()
	cfg.Running = 1
	q := New(held, cfg)
	defer q.Close()
	result := make(chan error, 1)
	go func() {
		lease, err := q.Acquire(context.Background(), nil)
		if lease != nil {
			lease.Release()
		}
		result <- err
	}()
	<-held.entered
	closed := make(chan struct{})
	go func() { q.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown waited for a blocked Redis admission")
	}
	proceed()
	if err := <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatalf("shutdown admitted work: %v", err)
	}
	other := New(store, cfg)
	defer other.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := other.Acquire(ctx, nil)
	if err != nil {
		t.Fatal("shutdown retained an orphan slot", err)
	}
	lease.Release()
}
