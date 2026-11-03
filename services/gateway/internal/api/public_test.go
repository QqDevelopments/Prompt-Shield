package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
)

type countingEngine struct {
	testEngine
	calls *atomic.Int32
}

func (e countingEngine) Inspect(ctx context.Context, request *pb.SanitizeRequest) (*pb.SanitizeResponse, error) {
	e.calls.Add(1)
	return e.testEngine.Inspect(ctx, request)
}

func enablePublic(s *Server) {
	s.config.PublicEnabled = true
	s.config.PublicHMACKey = "synthetic-hmac-secret-at-least-32-bytes"
}
func publicRequest(s *Server, key, text string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(proxyRequest{Model: "fixture", Messages: []message{{Role: "user", Content: text}}})
	r := httptest.NewRequest("POST", "/public/v1/chat/completions", strings.NewReader(string(body)))
	if key != "" {
		r.Header.Set("X-Provider-API-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestPublicRequiresOwnKeyAndIsolatesConcurrentCredentials(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body proxyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-"+body.Messages[0].Content {
			t.Error("cross-request credential mix")
		}
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer upstream.Close()
	s, repo := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	if w := publicRequest(s, "", "one"); w.Code != 401 {
		t.Fatalf("global key fallback: %d", w.Code)
	}
	var wg sync.WaitGroup
	for _, user := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := publicRequest(s, "synthetic-"+user, user); w.Code != 200 {
				t.Errorf("request failed: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal("unexpected upstream dispatch")
	}
	if w := publicRequest(s, "synthetic-one", "sk-fixture-secret"); w.Code != 400 {
		t.Fatalf("blocked: %d", w.Code)
	}
	if calls.Load() != 2 {
		t.Fatal("blocked request dispatched")
	}
	events, err := repo.Events(context.Background(), "org-a", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "synthetic-") {
		t.Fatal("credential leaked to audit")
	}
	for _, path := range []string{"/api/events", "/api/policy", "/api/keys"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("public management access: %s", path)
		}
	}
}
func TestPublicRedirectAndProviderErrorsDoNotLeakCredentials(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	for _, status := range []int{302, 401, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(r.Header.Get("Authorization")))
			}))
			defer upstream.Close()
			s, _ := fixture(t, testEngine{}, upstream.URL)
			enablePublic(s)
			w := publicRequest(s, "synthetic-private-provider-key", "hello")
			if w.Code < 400 || strings.Contains(w.Body.String(), "synthetic-private-provider-key") {
				t.Fatal("provider error exposed")
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("credential redirect followed")
	}
}
func TestPublicCORSAndSpoofedIPRateLimits(t *testing.T) {
	s, _ := fixture(t, testEngine{}, "https://provider.test/v1")
	enablePublic(s)
	s.config.AllowedOrigins = []string{"https://web.example.test"}
	s.config.PublicRPM = 1
	for _, origin := range []string{"https://web.example.test", "https://web.example.test.attacker.test", "null"} {
		r := httptest.NewRequest("OPTIONS", "/public/v1/inspect", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "content-type,x-provider-api-key,x-prompt-shield-progress")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if origin == "https://web.example.test" {
			if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatal("valid preflight failed")
			}
		} else if w.Code != 403 {
			t.Fatal("untrusted origin allowed")
		}
	}
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/public/v1/inspect", strings.NewReader(`{"text":"hello"}`))
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i+1))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if i == 0 && w.Code != 200 || i == 1 && w.Code != 429 {
			t.Fatalf("spoofed IP bypass: %d", w.Code)
		}
	}
}
func TestPublicDailyLimitStopsBeforeBackendWork(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, countingEngine{calls: &calls}, "https://provider.test/v1")
	enablePublic(s)
	s.config.PublicDailyPerIP = 5
	s.config.PublicRPM = 100
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/public/v1/inspect", strings.NewReader(`{"text":"hello"}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if i < 5 && w.Code != http.StatusOK {
			t.Fatalf("request %d rejected: %d", i+1, w.Code)
		}
		if i == 5 {
			if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "daily_limit_exceeded") {
				t.Fatalf("sixth request: %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Retry-After") == "" || w.Header().Get("X-RateLimit-Limit") != "5" {
				t.Fatal("daily quota response omitted reset metadata")
			}
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("daily rejection reached the engine: %d calls", calls.Load())
	}
}
func TestPublicDailyLimitCanBeDisabledForDevelopment(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, countingEngine{calls: &calls}, "https://provider.test/v1")
	enablePublic(s)
	s.config.PublicDailyPerIP = 0
	s.config.PublicRPM = 100
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/public/v1/inspect", strings.NewReader(`{"text":"hello"}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("development request %d rejected: %d", i+1, w.Code)
		}
	}
	if calls.Load() != 6 {
		t.Fatalf("development limit unexpectedly active: %d calls", calls.Load())
	}
}
func TestPublicConcurrencyRelease(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		respond(w, 200, map[string]any{})
	}))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	s.config.PublicPerIP = 1
	done := make(chan struct{})
	go func() { publicRequest(s, "synthetic-one", "hello"); close(done) }()
	<-entered
	if w := publicRequest(s, "synthetic-two", "hello"); w.Code != 429 {
		t.Errorf("concurrent admission: %d", w.Code)
	}
	close(release)
	<-done
	if w := publicRequest(s, "synthetic-two", "hello"); w.Code != 200 {
		t.Fatalf("lease not released: %d", w.Code)
	}
}
func TestPublicStreamingRestoresAndCancellationReleasesLease(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"[PS_test_EMAIL_1]\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	r := httptest.NewRequest("POST", "/public/v1/chat/completions", strings.NewReader(`{"model":"fixture","stream":true,"messages":[{"role":"user","content":"alice@example.com"}]}`))
	r.Header.Set("X-Provider-API-Key", "synthetic-stream-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "alice@example.com") || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatalf("public stream failed: %d %s", w.Code, w.Body.String())
	}
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer slow.Close()
	s.config.OpenAIURL = slow.URL
	s.config.PublicPerIP = 1
	ctx, cancel := context.WithCancel(context.Background())
	r = httptest.NewRequest("POST", "/public/v1/chat/completions", strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`)).WithContext(ctx)
	r.Header.Set("X-Provider-API-Key", "synthetic-cancel-key")
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-entered
	cancel()
	<-done
	<-cancelled
	s.config.OpenAIURL = upstream.URL
	if w := publicRequest(s, "synthetic-next-key", "hello"); w.Code == 429 {
		t.Fatal("cancelled request retained lease")
	}
}
func TestHostedIntegrationCannotUseEnvironmentKey(t *testing.T) {
	s, _ := fixture(t, testEngine{}, "https://provider.test/v1")
	s.config.Hosted = true
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`))
	r = r.WithContext(context.WithValue(r.Context(), orgKey{}, "org-a"))
	w := httptest.NewRecorder()
	s.proxy(w, r)
	if w.Code != 401 {
		t.Fatal("hosted environment credential accepted")
	}
}
func TestTrustedProxyWalkUsesFirstUntrustedHop(t *testing.T) {
	s, _ := fixture(t, testEngine{}, "https://provider.test/v1")
	s.config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	for _, tc := range []struct{ peer, forwarded, want string }{{"192.0.2.5:1000", "198.51.100.1", "192.0.2.5"}, {"10.0.0.2:1000", "203.0.113.1, 198.51.100.7, 10.0.0.3", "198.51.100.7"}, {"10.0.0.2:1000", "invalid", ""}} {
		r := httptest.NewRequest("POST", "/public/v1/inspect", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		got, ok := s.clientIP(r)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("client IP got %q valid %v", got, ok)
		}
	}
}
