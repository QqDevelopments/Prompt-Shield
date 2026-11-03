package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type metrics struct {
	allowed       atomic.Uint64
	masked        atomic.Uint64
	blocked       atomic.Uint64
	auditFailures atomic.Uint64
	count         atomic.Uint64
	micros        atomic.Uint64
}

func (s *Server) recordDecision(decision string, elapsed time.Duration) {
	switch decision {
	case "allowed":
		s.metrics.allowed.Add(1)
	case "masked":
		s.metrics.masked.Add(1)
	case "blocked":
		s.metrics.blocked.Add(1)
	}
	s.metrics.count.Add(1)
	s.metrics.micros.Add(uint64(elapsed.Microseconds()))
}
func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	expected := "Bearer " + s.config.MetricsToken
	if len(s.config.MetricsToken) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
		fail(w, 401, "unauthorized")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP prompt_shield_decisions_total Recorded security decisions.\n# TYPE prompt_shield_decisions_total counter\nprompt_shield_decisions_total{decision=\"allowed\"} %d\nprompt_shield_decisions_total{decision=\"masked\"} %d\nprompt_shield_decisions_total{decision=\"blocked\"} %d\n", s.metrics.allowed.Load(), s.metrics.masked.Load(), s.metrics.blocked.Load())
	fmt.Fprintf(w, "# TYPE prompt_shield_inspection_seconds summary\nprompt_shield_inspection_seconds_count %d\nprompt_shield_inspection_seconds_sum %f\n# TYPE prompt_shield_audit_failures_total counter\nprompt_shield_audit_failures_total %d\n", s.metrics.count.Load(), float64(s.metrics.micros.Load())/1e6, s.metrics.auditFailures.Load())
}
