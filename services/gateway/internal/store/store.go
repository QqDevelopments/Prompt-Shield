package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not_found")
var ErrConflict = errors.New("policy_conflict")

type Policy struct {
	Version         int      `json:"version"`
	MaskPII         bool     `json:"mask_pii"`
	BlockSecrets    bool     `json:"block_secrets"`
	ContextualPII   bool     `json:"contextual_pii"`
	BlockedKeywords []string `json:"blocked_keywords"`
}

func DefaultPolicy() Policy {
	return Policy{Version: 1, MaskPII: true, BlockSecrets: true, BlockedKeywords: []string{}}
}

type Key struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	Hash      string    `json:"-"`
}
type Event struct {
	Completion
	ID          string    `json:"id"`
	Decision    string    `json:"decision"`
	Reason      string    `json:"reason"`
	EntityCount uint32    `json:"entity_count"`
	EntityTypes []string  `json:"entity_types"`
	LatencyMS   int64     `json:"latency_ms"`
	CreatedAt   time.Time `json:"created_at"`
}
type Repository interface {
	Policy(context.Context, string) (Policy, error)
	SavePolicy(context.Context, string, Policy) error
	Keys(context.Context, string) ([]Key, error)
	AddKey(context.Context, string, Key) error
	RevokeKey(context.Context, string, string) error
	Authenticate(context.Context, string) (string, error)
	AddEvent(context.Context, string, Event) error
	Events(context.Context, string, int, int, ...string) ([]Event, error)
	Ready(context.Context) error
	Complete(context.Context, string, string, Completion) error
}

func Token() string { return rand.Text() + rand.Text() }
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Memory is explicitly for ephemeral local development and tests.
type Memory struct {
	mu        sync.RWMutex
	policies  map[string]Policy
	keys      map[string][]Key
	events    map[string][]Event
	eventHead map[string]int
}

func NewMemory() *Memory {
	return &Memory{policies: map[string]Policy{}, keys: map[string][]Key{}, events: map[string][]Event{}, eventHead: map[string]int{}}
}
func copyPolicy(p Policy) Policy {
	p.BlockedKeywords = append([]string{}, p.BlockedKeywords...)
	return p
}
func (m *Memory) Policy(_ context.Context, org string) (Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[org]
	if !ok {
		p = DefaultPolicy()
	}
	return copyPolicy(p), nil
}
func (m *Memory) SavePolicy(_ context.Context, org string, p Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.policies[org]
	if !ok {
		current = DefaultPolicy()
	}
	if current.Version != p.Version {
		return ErrConflict
	}
	p.Version++
	m.policies[org] = copyPolicy(p)
	return nil
}
func (m *Memory) Keys(_ context.Context, org string) ([]Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Key{}, m.keys[org]...), nil
}
func (m *Memory) AddKey(_ context.Context, org string, k Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.keys[org]) >= 1000 {
		return errors.New("key_limit")
	}
	m.keys[org] = append(m.keys[org], k)
	return nil
}
func (m *Memory) RevokeKey(_ context.Context, org, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.keys[org] {
		if m.keys[org][i].ID == id {
			m.keys[org][i].Active = false
			return nil
		}
	}
	return ErrNotFound
}
func (m *Memory) Authenticate(_ context.Context, hash string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for org, keys := range m.keys {
		for _, key := range keys {
			if key.Active && key.Hash == hash {
				return org, nil
			}
		}
	}
	return "", ErrNotFound
}
func (m *Memory) AddEvent(_ context.Context, org string, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.EntityTypes = append([]string{}, e.EntityTypes...)
	if e.Outcome == "" {
		e.Outcome = "inspected"
	}
	if len(m.events[org]) < 10000 {
		m.events[org] = append(m.events[org], e)
	} else {
		head := m.eventHead[org]
		m.events[org][head] = e
		m.eventHead[org] = (head + 1) % 10000
	}
	return nil
}
func (m *Memory) Events(_ context.Context, org string, limit, offset int, decision ...string) ([]Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []Event{}
	items := m.events[org]
	for i := len(items) - 1; i >= 0 && len(result) < limit; i-- {
		e := items[(m.eventHead[org]+i)%len(items)]
		if len(decision) > 0 && decision[0] != "" && e.Decision != decision[0] {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		e.EntityTypes = append([]string{}, e.EntityTypes...)
		result = append(result, e)
	}
	return result, nil
}
func (m *Memory) Ready(context.Context) error { return nil }
