package workqueue

import (
	"context"
	"sort"
	"sync"
	"time"
)

type ticket struct {
	order   uint64
	expires time.Time
	running bool
}
type Memory struct {
	mu          sync.Mutex
	entries     map[string]ticket
	next        uint64
	fingerprint string
	watchers    map[chan struct{}]bool
}

func NewMemory() *Memory {
	return &Memory{entries: map[string]ticket{}, watchers: map[chan struct{}]bool{}}
}
func (m *Memory) notify() {
	for ch := range m.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (m *Memory) prepare(s Settings) error {
	if m.fingerprint != "" && m.fingerprint != s.fingerprint() {
		return ErrConfig
	}
	m.fingerprint = s.fingerprint()
	now := time.Now()
	for id, t := range m.entries {
		if !now.Before(t.expires) {
			delete(m.entries, id)
		}
	}
	return nil
}
func (m *Memory) promote(s Settings) {
	running := 0
	waiting := []string{}
	for id, t := range m.entries {
		if t.running {
			running++
		} else {
			waiting = append(waiting, id)
		}
	}
	sort.Slice(waiting, func(i, j int) bool { return m.entries[waiting[i]].order < m.entries[waiting[j]].order })
	for _, id := range waiting {
		if running >= s.Running {
			break
		}
		t := m.entries[id]
		t.running = true
		t.expires = time.Now().Add(s.Processing + 5*time.Second)
		m.entries[id] = t
		running++
		m.notify()
	}
}
func (m *Memory) state(id string) Snapshot {
	t, ok := m.entries[id]
	if !ok {
		return Snapshot{Err: ErrUnavailable}
	}
	if t.running {
		return Snapshot{Remaining: time.Until(t.expires)}
	}
	position := 1
	for _, other := range m.entries {
		if !other.running && other.order < t.order {
			position++
		}
	}
	return Snapshot{Position: position}
}
func (m *Memory) Join(ctx context.Context, id string, s Settings) (Snapshot, error) {
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.prepare(s); err != nil {
		return Snapshot{}, err
	}
	m.promote(s)
	running, waiting := 0, 0
	for _, t := range m.entries {
		if t.running {
			running++
		} else {
			waiting++
		}
	}
	if running >= s.Running && waiting >= s.Waiting {
		return Snapshot{}, ErrFull
	}
	m.next++
	m.entries[id] = ticket{order: m.next, expires: time.Now().Add(s.OwnerTTL)}
	m.promote(s)
	m.notify()
	return m.state(id), nil
}
func (m *Memory) Sync(ctx context.Context, ids []string, s Settings) (map[string]Snapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.prepare(s); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if t, ok := m.entries[id]; ok && !t.running {
			t.expires = time.Now().Add(s.OwnerTTL)
			m.entries[id] = t
		}
	}
	m.promote(s)
	out := map[string]Snapshot{}
	for _, id := range ids {
		out[id] = m.state(id)
	}
	return out, nil
}
func (m *Memory) Remove(_ context.Context, ids ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.entries, id)
	}
	m.notify()
	return nil
}
func (m *Memory) Subscribe(context.Context) (<-chan struct{}, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan struct{}, 1)
	m.watchers[ch] = true
	return ch, func() { m.mu.Lock(); defer m.mu.Unlock(); delete(m.watchers, ch) }
}
