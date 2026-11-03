package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentReservations(t *testing.T) {
	limiter := NewMemory()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := limiter.Allow(context.Background(), "organization", 10)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("admitted %d", allowed.Load())
	}
}

func TestTokenBudgetReservationIsAtomic(t *testing.T) {
	limiter := NewMemory()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := limiter.Reserve(context.Background(), "org", 100, 1000)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("budget admitted %d", allowed.Load())
	}
}
