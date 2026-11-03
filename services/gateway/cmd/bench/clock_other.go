//go:build !windows

package main

import "time"

func newMeasurementClock() (measurementClock, error) {
	origin := time.Now()
	now := func() int64 { return time.Since(origin).Nanoseconds() }
	return measurementClock{now: now, since: func(start int64) time.Duration { return time.Duration(now() - start) },
		name: "Go monotonic clock", frequency: int64(time.Second)}, nil
}
