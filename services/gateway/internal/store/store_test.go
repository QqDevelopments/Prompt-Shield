package store

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestOrganizationIsolation(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	if err := r.AddKey(ctx, "a", Key{ID: "key", Hash: Hash("secret"), Active: true}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := r.Keys(ctx, "b"); len(keys) != 0 {
		t.Fatal("key isolation failed")
	}
	if err := r.RevokeKey(ctx, "b", "key"); err == nil {
		t.Fatal("cross-organization revocation")
	}
	if err := r.AddEvent(ctx, "a", Event{ID: "event", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if events, _ := r.Events(ctx, "b", 50, 0); len(events) != 0 {
		t.Fatal("event isolation failed")
	}
	p := DefaultPolicy()
	p.MaskPII = false
	if err := r.SavePolicy(ctx, "a", p); err != nil {
		t.Fatal(err)
	}
	other, _ := r.Policy(ctx, "b")
	if !other.MaskPII {
		t.Fatal("policy isolation failed")
	}
}

func TestBoundedEventsPreserveFilteredPaginationAfterWrap(t *testing.T) {
	repo := NewMemory()
	ctx := context.Background()
	for i := 0; i < 10005; i++ {
		decision := "allowed"
		if i%2 == 0 {
			decision = "masked"
		}
		if err := repo.AddEvent(ctx, "a", Event{ID: strconv.Itoa(i), Decision: decision}); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := repo.Events(ctx, "a", 10001, 0)
	if len(events) != 10000 || events[0].ID != "10004" || events[len(events)-1].ID != "5" {
		t.Fatal("retention order")
	}
	events, _ = repo.Events(ctx, "a", 2, 1, "masked")
	if len(events) != 2 || events[0].ID != "10002" || events[1].ID != "10000" {
		t.Fatal("filter before pagination")
	}
	if err := repo.Complete(ctx, "a", "10004", Completion{Outcome: "completed"}); err != nil {
		t.Fatal(err)
	}
	events, _ = repo.Events(ctx, "a", 1, 0)
	if events[0].Outcome != "completed" {
		t.Fatal("wrapped completion")
	}
}
