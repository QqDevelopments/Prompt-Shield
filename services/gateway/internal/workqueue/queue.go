package workqueue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"
)

var (
	ErrFull        = errors.New("queue_full")
	ErrTimeout     = errors.New("queue_timeout")
	ErrUnavailable = errors.New("queue_unavailable")
	ErrConfig      = errors.New("queue_configuration_mismatch")
)

const ProcessingTimeout = 70 * time.Second
const reconcileInterval = 2 * time.Second

type Settings struct {
	Running, Waiting           int
	Wait, Processing, OwnerTTL time.Duration
}

func Defaults() Settings {
	return Settings{2, 20, 30 * time.Second, ProcessingTimeout, 6 * time.Second}
}

func FromEnv(get func(string) string) (Settings, error) {
	s := Defaults()
	for _, item := range []struct {
		name     string
		target   *int
		min, max int
	}{
		{"QUEUE_MAX_RUNNING", &s.Running, 1, 64}, {"QUEUE_MAX_WAITING", &s.Waiting, 0, 256},
	} {
		if raw := get(item.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < item.min || n > item.max {
				return s, errors.New("invalid_" + item.name)
			}
			*item.target = n
		}
	}
	if raw := get("QUEUE_WAIT_TIMEOUT_SECONDS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 300 {
			return s, errors.New("invalid_QUEUE_WAIT_TIMEOUT_SECONDS")
		}
		s.Wait = time.Duration(n) * time.Second
	}
	return s, nil
}

func Bool(get func(string) string, name string, fallback bool) (bool, error) {
	switch get(name) {
	case "":
		return fallback, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("invalid_" + name)
	}
}

func (s Settings) fingerprint() string {
	return "v1:" + strconv.Itoa(s.Running) + ":" + strconv.Itoa(s.Waiting) + ":" + strconv.FormatInt(s.Wait.Milliseconds(), 10) + ":" + strconv.FormatInt(s.Processing.Milliseconds(), 10) + ":" + strconv.FormatInt(s.OwnerTTL.Milliseconds(), 10)
}

type Snapshot struct {
	Position  int
	Remaining time.Duration
	Err       error
}
type Store interface {
	Join(context.Context, string, Settings) (Snapshot, error)
	Sync(context.Context, []string, Settings) (map[string]Snapshot, error)
	Remove(context.Context, ...string) error
	Subscribe(context.Context) (<-chan struct{}, func())
}
type reference struct {
	updates chan Snapshot
	cancel  context.CancelCauseFunc
	err     error
}
type Scheduler struct {
	store           Store
	Settings        Settings
	mu              sync.Mutex
	refs            map[string]*reference
	cleanup         map[string]time.Time
	wake            chan struct{}
	running, closed bool
}

func New(store Store, settings Settings) *Scheduler {
	return &Scheduler{store: store, Settings: settings, refs: map[string]*reference{}, cleanup: map[string]time.Time{}, wake: make(chan struct{}, 1)}
}
func (s *Scheduler) Ready(ctx context.Context) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrUnavailable
	}
	_, err := s.store.Sync(ctx, nil, s.Settings)
	return err
}
func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Retain only an opaque cleanup ticket when Redis cannot acknowledge removal.
func (s *Scheduler) remove(id string) {
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	err := s.store.Remove(ctx, id)
	done()
	if err != nil {
		s.mu.Lock()
		if !s.closed {
			s.cleanup[id] = time.Now().Add(s.Settings.Processing + 5*time.Second)
			if !s.running {
				s.running = true
				go s.loop()
			}
		}
		s.mu.Unlock()
	}
	s.signal()
}
func update(r *reference, v Snapshot) {
	select {
	case <-r.updates:
	default:
	}
	r.updates <- v
}

type Lease struct {
	Context context.Context
	release func()
}

func (l *Lease) Release() { l.release() }

// Only opaque tickets reach Store. Bodies and credentials remain in their HTTP request.
func (s *Scheduler) Acquire(ctx context.Context, position func(int)) (*Lease, error) {
	wait, cancel := context.WithTimeout(ctx, s.Settings.Wait)
	defer cancel()
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, ErrUnavailable
	}
	id := hex.EncodeToString(raw[:])
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	s.mu.Unlock()
	join, joinDone := context.WithTimeout(wait, time.Second)
	initial, err := s.store.Join(join, id, s.Settings)
	joinDone()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		_ = s.store.Remove(cleanup, id)
		return nil, ErrUnavailable
	}
	r := &reference{updates: make(chan Snapshot, 1)}
	r.updates <- initial
	s.refs[id] = r
	if !s.running {
		s.running = true
		go s.loop()
	}
	s.mu.Unlock()
	s.signal()
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.refs, id)
			if r.cancel != nil {
				r.cancel(context.Canceled)
			}
			s.mu.Unlock()
			s.remove(id)
		})
	}
	last := -1
	for {
		select {
		case <-wait.Done():
			release()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrTimeout
		case state := <-r.updates:
			if state.Err != nil {
				release()
				return nil, state.Err
			}
			if state.Position > 0 {
				if state.Position != last && position != nil {
					position(state.Position)
				}
				last = state.Position
				continue
			}
			if ctx.Err() != nil {
				release()
				return nil, ctx.Err()
			}
			if wait.Err() != nil {
				release()
				return nil, ErrTimeout
			}
			budget := min(s.Settings.Processing, state.Remaining-5*time.Second)
			if budget <= 0 {
				release()
				return nil, ErrUnavailable
			}
			timed, done := context.WithTimeout(ctx, budget)
			processing, stop := context.WithCancelCause(timed)
			s.mu.Lock()
			r.cancel = stop
			cause := r.err
			if s.closed {
				cause = ErrUnavailable
			}
			s.mu.Unlock()
			if cause != nil {
				stop(cause)
				done()
				release()
				return nil, cause
			}
			return &Lease{Context: processing, release: func() { done(); release() }}, nil
		}
	}
}

func (s *Scheduler) loop() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notices, stop := s.store.Subscribe(ctx)
	defer stop()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		for id, until := range s.cleanup {
			if !time.Now().Before(until) {
				delete(s.cleanup, id)
			}
		}
		if (len(s.refs) == 0 && len(s.cleanup) == 0) || s.closed {
			s.running = false
			s.mu.Unlock()
			return
		}
		ids := make([]string, 0, len(s.refs))
		for id := range s.refs {
			ids = append(ids, id)
		}
		pending := make([]string, 0, len(s.cleanup))
		for id := range s.cleanup {
			pending = append(pending, id)
		}
		s.mu.Unlock()
		if len(pending) > 0 {
			call, done := context.WithTimeout(ctx, time.Second)
			err := s.store.Remove(call, pending...)
			done()
			if err == nil {
				s.mu.Lock()
				for _, id := range pending {
					delete(s.cleanup, id)
				}
				s.mu.Unlock()
				if len(ids) == 0 {
					continue
				}
			}
		}
		call, done := context.WithTimeout(ctx, time.Second)
		states, err := s.store.Sync(call, ids, s.Settings)
		done()
		s.mu.Lock()
		for _, id := range ids {
			r := s.refs[id]
			if r == nil {
				continue
			}
			state, ok := states[id]
			if err != nil || !ok {
				state = Snapshot{Err: ErrUnavailable}
			}
			if state.Err != nil {
				r.err = state.Err
			}
			if r.err != nil {
				state.Err = r.err
				if r.cancel != nil {
					r.cancel(r.err)
				}
			}
			update(r, state)
		}
		s.mu.Unlock()
		select {
		case <-ticker.C:
		case <-notices:
		case <-s.wake:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	for _, r := range s.refs {
		if r.cancel != nil {
			r.cancel(ErrUnavailable)
		}
		update(r, Snapshot{Err: ErrUnavailable})
	}
	s.mu.Unlock()
	s.signal()
}
