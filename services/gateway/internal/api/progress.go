package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/workqueue"
)

type progressWriter struct {
	target  http.ResponseWriter
	headers http.Header
	status  int
	body    bytes.Buffer
	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
}

func (p *progressWriter) Header() http.Header { return p.headers }
func (p *progressWriter) WriteHeader(status int) {
	if p.status == 0 {
		p.status = status
	}
}
func (p *progressWriter) Write(b []byte) (int, error) {
	if p.status == 0 {
		p.status = 200
	}
	if p.body.Len()+len(b) > 2*1024*1024 {
		return 0, errors.New("response_too_large")
	}
	return p.body.Write(b)
}
func (p *progressWriter) event(name string, value any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		p.target.Header().Set("Content-Type", "text/event-stream")
		p.target.Header().Set("Cache-Control", "no-store")
		p.target.Header().Set("X-Accel-Buffering", "no")
		p.target.WriteHeader(200)
		p.started = true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	defer clear(raw)
	if _, err = io.WriteString(p.target, "event: "+name+"\ndata: "); err == nil {
		_, err = p.target.Write(raw)
	}
	if err == nil {
		_, err = io.WriteString(p.target, "\n\n")
	}
	if err == nil {
		err = http.NewResponseController(p.target).Flush()
	}
	if err != nil {
		p.cancel()
	}
}
func (p *progressWriter) finish() {
	defer func() { clear(p.body.Bytes()); p.body.Reset() }()
	if !p.started {
		if p.status == 0 {
			p.status = http.StatusOK
		}
		for k, v := range p.headers {
			p.target.Header()[k] = v
		}
		p.target.WriteHeader(p.status)
		_, _ = p.target.Write(p.body.Bytes())
		return
	}
	if !json.Valid(p.body.Bytes()) {
		p.event("error", map[string]any{"http_status": 502, "error": map[string]string{"code": "invalid_response"}})
		return
	}
	if p.status >= 400 {
		p.event("error", map[string]any{"http_status": p.status, "response": json.RawMessage(p.body.Bytes())})
	} else {
		p.event("result", json.RawMessage(p.body.Bytes()))
	}
}

func (p *progressWriter) keepAlive() func() {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				p.mu.Lock()
				var err error
				if p.started {
					_, err = io.WriteString(p.target, ": keepalive\n\n")
					if err == nil {
						err = http.NewResponseController(p.target).Flush()
					}
				}
				p.mu.Unlock()
				if err != nil {
					p.cancel()
					return
				}
			}
		}
	}()
	return func() { close(done); <-exited }
}

func validProgress(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("X-Prompt-Shield-Progress")
	if len(values) > 1 || (len(values) == 1 && values[0] != "v1") {
		fail(w, 400, "invalid_progress_mode")
		return false
	}
	return true
}

// The progress envelope is opt-in; provider-native JSON and SSE are unchanged.
func (s *Server) work(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request, func(), bool) {
	values := r.Header.Values("X-Prompt-Shield-Progress")
	if !validProgress(w, r) {
		return nil, r, func() {}, false
	}
	ctx, cancel := context.WithCancel(r.Context())
	var progress *progressWriter
	stopHeartbeat := func() {}
	if len(values) == 1 {
		progress = &progressWriter{target: w, headers: make(http.Header), cancel: cancel}
		stopHeartbeat = progress.keepAlive()
	}
	position := func(n int) {
		if progress != nil {
			progress.event("queued", map[string]int{"position": n})
		}
	}
	lease, err := s.config.Queue.Acquire(ctx, position)
	if err != nil {
		stopHeartbeat()
		code, status := "queue_unavailable", 503
		if errors.Is(err, workqueue.ErrTimeout) {
			code = "queue_timeout"
		}
		if errors.Is(err, workqueue.ErrConfig) {
			code = "queue_configuration_mismatch"
		}
		if errors.Is(err, workqueue.ErrFull) {
			status = 429
			code = "queue_full"
		}
		if ctx.Err() != nil {
			code = "request_cancelled"
		}
		if progress != nil && progress.started {
			fail(progress, status, code)
			progress.finish()
		} else {
			w.Header().Set("Retry-After", "1")
			fail(w, status, code)
		}
		cancel()
		return nil, r, func() {}, false
	}
	if progress == nil {
		if deadline, ok := lease.Context.Deadline(); ok {
			_ = http.NewResponseController(w).SetWriteDeadline(deadline)
		}
		return w, r.WithContext(lease.Context), func() { lease.Release(); cancel() }, true
	}
	if deadline, ok := lease.Context.Deadline(); ok {
		_ = http.NewResponseController(w).SetWriteDeadline(deadline)
	}
	progress.event("processing", map[string]string{"state": "processing"})
	return progress, r.WithContext(lease.Context), func() {
		stopHeartbeat()
		if cause := context.Cause(lease.Context); cause != nil && r.Context().Err() == nil {
			clear(progress.body.Bytes())
			progress.body.Reset()
			code := "request_timeout"
			if errors.Is(cause, workqueue.ErrUnavailable) {
				code = "queue_unavailable"
			}
			progress.status = 0
			fail(progress, 503, code)
		}
		if ctx.Err() == nil {
			progress.finish()
		}
		clear(progress.body.Bytes())
		progress.body.Reset()
		lease.Release()
		cancel()
	}, true
}
