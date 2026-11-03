package api

import (
	"bufio"
	"context"
	"encoding/json"
	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/workqueue"
	"github.com/redis/go-redis/v9"
)

func TestProgressProviderResultsAndSafeFailures(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), "alice@example.com") {
					t.Error("unmasked provider request")
				}
				if provider == "anthropic" {
					respond(w, 200, map[string]any{"content": []any{map[string]string{"type": "text", "text": "[PS_test_EMAIL_1] [PS_test_EMAIL_1]"}}})
				} else {
					respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "[PS_test_EMAIL_1] [PS_test_EMAIL_1]"}}}})
				}
			}))
			defer upstream.Close()
			s, _ := fixture(t, testEngine{}, upstream.URL)
			enablePublic(s)
			path := "/public/v1/chat/completions"
			if provider == "anthropic" {
				path = "/public/v1/messages"
			}
			if provider == "gemini" {
				path = "/public/v1/gemini/chat/completions"
			}
			request := func(text, mode string, stream bool) *httptest.ResponseRecorder {
				body, _ := json.Marshal(proxyRequest{Model: "fixture", Messages: []message{{Role: "user", Content: text}}, Stream: stream})
				r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
				r.Header.Set("X-Provider-API-Key", "synthetic-provider-key")
				r.Header.Set("X-Prompt-Shield-Progress", mode)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			result := request("alice@example.com", "v1", false)
			if result.Code != 200 || result.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(result.Body.String(), "event: processing") || !strings.Contains(result.Body.String(), "event: result") || strings.Count(result.Body.String(), "alice@example.com") != 2 {
				t.Fatalf("restored result missing: %d %s", result.Code, result.Body.String())
			}
			for _, mode := range []string{"v1", "v2"} {
				if w := request("hello", mode, true); w.Code != 400 {
					t.Fatalf("incompatible progress request accepted: %d", w.Code)
				}
			}
			blocked := request("sk-fixture-secret", "v1", false)
			if !strings.Contains(blocked.Body.String(), "event: error") || !strings.Contains(blocked.Body.String(), "prompt_shield_blocked") || strings.Contains(blocked.Body.String(), "sk-fixture-secret") || calls.Load() != 1 {
				t.Fatalf("unsafe rejection: %s, calls %d", blocked.Body.String(), calls.Load())
			}
		})
	}
}

func TestTwoGatewayHTTPQueueSharesRedisAndCancelsWithoutDispatch(t *testing.T) {
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL required for two gateway Redis integration")
	}
	options, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	var inspections atomic.Int32
	entered, proceed := make(chan struct{}, 1), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-proceed:
		case <-r.Context().Done():
			return
		}
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "[PS_test_EMAIL_1]"}}}})
	}))
	defer upstream.Close()
	namespace := "http-queue-test-" + store.Token()
	cfg := workqueue.Defaults()
	cfg.Running = 1
	cfg.Waiting = 1
	cfg.Wait = 4 * time.Second
	queues := []*workqueue.Scheduler{workqueue.New(workqueue.NewRedis(client, namespace), cfg), workqueue.New(workqueue.NewRedis(client, namespace), cfg)}
	servers := make([]*httptest.Server, 2)
	for i := range servers {
		s, _ := fixture(t, countingEngine{testEngine{}, &inspections}, upstream.URL)
		enablePublic(s)
		s.config.DisableIPRateLimits = true
		s.config.Queue = queues[i]
		servers[i] = httptest.NewServer(s.Handler())
		defer servers[i].Close()
		defer queues[i].Close()
	}
	send := func(ctx context.Context, index int, path, body, progress string) (*http.Response, error) {
		r, e := http.NewRequestWithContext(ctx, "POST", servers[index].URL+path, strings.NewReader(body))
		if e != nil {
			return nil, e
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Provider-API-Key", "synthetic-provider-key")
		if progress != "" {
			r.Header.Set("X-Prompt-Shield-Progress", progress)
		}
		return http.DefaultClient.Do(r)
	}
	first := make(chan error, 1)
	go func() {
		response, e := send(ctx, 0, "/public/v1/chat/completions", `{"model":"fixture","messages":[{"role":"user","content":"alice@example.com"}]}`, "")
		if e == nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 200 || !strings.Contains(string(body), "alice@example.com") {
				e = io.ErrUnexpectedEOF
			}
		}
		first <- e
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("provider was not called")
	}
	waitCtx, cancelWait := context.WithCancel(ctx)
	waiting, err := send(waitCtx, 1, "/public/v1/inspect", `{"text":"cancelled payload"}`, "v1")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(waiting.Body)
	if !scanner.Scan() || scanner.Text() != "event: queued" {
		t.Fatal("second gateway did not share the running slot")
	}
	full, err := send(ctx, 0, "/public/v1/inspect", `{"text":"full queue payload"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(full.Body)
	full.Body.Close()
	if full.StatusCode != 429 || !strings.Contains(string(body), "queue_full") {
		t.Fatalf("queue overflow: %d %s", full.StatusCode, body)
	}
	denied, err := send(ctx, 1, "/v1/inspect", `{"text":"unauthorized"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatal("unauthorized request entered the queue")
	}
	cancelWait()
	waiting.Body.Close()
	probe := workqueue.NewRedis(client, namespace)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = probe.Join(ctx, "cleanup-probe", cfg)
		if err == nil {
			probe.Remove(ctx, "cleanup-probe")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected waiter was not removed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if inspections.Load() != 1 {
		t.Fatal("queued or rejected request reached the engine")
	}
	close(proceed)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	next, err := send(ctx, 1, "/public/v1/inspect", `{"text":"recovery"}`, "v1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(next.Body)
	next.Body.Close()
	if !strings.Contains(string(body), "event: processing") || !strings.Contains(string(body), "event: result") || inspections.Load() != 2 {
		t.Fatalf("recovery failed: %s", body)
	}
}

func TestDisabledPublicIPChecksRetainGlobalCapacity(t *testing.T) {
	entered, proceed := make(chan struct{}, 3), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-proceed:
		case <-r.Context().Done():
			return
		}
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	s.config.DisableIPRateLimits = true
	s.config.PublicRPM = 1
	s.config.PublicPerIP = 1
	s.config.PublicGlobal = 2
	finished := make(chan int, 2)
	for range 2 {
		go func() { finished <- publicRequest(s, "synthetic-key", "hello").Code }()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("disabled IP limit still rejected same-IP requests")
		}
	}
	full := publicRequest(s, "synthetic-key", "hello")
	if full.Code != 429 || !strings.Contains(full.Body.String(), "concurrency_limited") {
		t.Fatalf("global capacity disabled: %d %s", full.Code, full.Body.String())
	}
	close(proceed)
	for range 2 {
		if code := <-finished; code != 200 {
			t.Fatalf("request failed: %d", code)
		}
	}
}

func TestProgressWaitTimeoutDoesNotReachEngineAndThenRecovers(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, countingEngine{testEngine{}, &calls}, "https://provider.test/v1")
	enablePublic(s)
	cfg := workqueue.Defaults()
	cfg.Running = 1
	cfg.Wait = 30 * time.Millisecond
	s.config.Queue = workqueue.New(workqueue.NewMemory(), cfg)
	defer s.config.Queue.Close()
	lease, err := s.config.Queue.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/public/v1/inspect", strings.NewReader(`{"text":"queued"}`))
		r.Header.Set("X-Prompt-Shield-Progress", "v1")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request()
	if !strings.Contains(w.Body.String(), "event: queued") || !strings.Contains(w.Body.String(), "queue_timeout") || strings.Contains(w.Body.String(), "event: processing") || calls.Load() != 0 {
		t.Fatalf("timeout dispatched work: %s", w.Body.String())
	}
	lease.Release()
	w = request()
	if !strings.Contains(w.Body.String(), "event: result") || calls.Load() != 1 {
		t.Fatal("timed-out request blocked recovery")
	}
}

type deadlineEngine struct{ testEngine }

func (e deadlineEngine) Inspect(ctx context.Context, _ *pb.SanitizeRequest) (*pb.SanitizeResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProgressProcessingBudgetCancelsEngine(t *testing.T) {
	s, _ := fixture(t, deadlineEngine{}, "https://provider.test/v1")
	enablePublic(s)
	cfg := workqueue.Defaults()
	cfg.Processing = 30 * time.Millisecond
	s.config.Queue = workqueue.New(workqueue.NewMemory(), cfg)
	defer s.config.Queue.Close()
	r := httptest.NewRequest("POST", "/public/v1/inspect", strings.NewReader(`{"text":"bounded execution"}`))
	r.Header.Set("X-Prompt-Shield-Progress", "v1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "request_timeout") || !strings.Contains(w.Body.String(), `"http_status":503`) || strings.Contains(w.Body.String(), "event: result") {
		t.Fatalf("processing deadline was not safe: %s", w.Body.String())
	}
}

func TestEmbeddingsShareProtectionCapacityAndRecoverAfterWaitTimeout(t *testing.T) {
	var calls, inspections atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "alice@example.com") {
			t.Error("embedding input was not sanitized")
		}
		respond(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.5}}}, "model": "fixture", "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	defer upstream.Close()
	s, repo := fixture(t, countingEngine{testEngine{}, &inspections}, upstream.URL)
	secret := "ps_" + store.Token()
	if err := repo.AddKey(context.Background(), s.config.Organization, store.Key{ID: store.Token(), Hash: store.Hash(secret), Active: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	cfg := workqueue.Defaults()
	cfg.Running = 1
	cfg.Wait = 30 * time.Millisecond
	s.config.Queue = workqueue.New(workqueue.NewMemory(), cfg)
	defer s.config.Queue.Close()
	lease, err := s.config.Queue.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/embeddings", strings.NewReader(`{"model":"fixture","input":"alice@example.com"}`))
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 503 || !strings.Contains(w.Body.String(), "queue_timeout") || calls.Load() != 0 || inspections.Load() != 0 {
		t.Fatalf("embedding bypassed the queue: %d %s", w.Code, w.Body.String())
	}
	lease.Release()
	w = request()
	if w.Code != 200 || calls.Load() != 1 || inspections.Load() != 1 {
		t.Fatalf("embedding did not recover: %d %s", w.Code, w.Body.String())
	}
}
