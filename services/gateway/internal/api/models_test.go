package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func modelRequest(s *Server, provider string, keys ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/public/v1/models", strings.NewReader(`{"provider":"`+provider+`"}`))
	for _, key := range keys {
		r.Header.Add("X-Provider-API-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestModelsProviderCredentialsPaginationAndIsolation(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.URL.Path != "/models" || r.Header.Get("Cookie") != "" {
					t.Error("unexpected upstream request")
				}
				if provider != "anthropic" {
					if r.Header.Get("Authorization") != "Bearer synthetic-list-key" || r.Header.Get("x-api-key") != "" {
						t.Error("wrong credential")
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"gpt-older-luna","created":1},{"id":"gpt-newer-luna","created":2},{"id":"gpt-newer-luna","created":2},{"id":"invalid id"}],"secret":"must-not-return"}`))
					return
				}
				if r.Header.Get("x-api-key") != "synthetic-list-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
					t.Error("wrong Anthropic headers")
				}
				if r.URL.Query().Get("limit") != "1000" {
					t.Error("missing page limit")
				}
				if r.URL.Query().Get("after_id") == "" {
					_, _ = w.Write([]byte(`{"data":[{"id":"claude-newer-sonnet","display_name":"New Sonnet","created_at":"2026-01-01T00:00:00Z"}],"has_more":true,"last_id":"claude-newer-sonnet"}`))
				} else {
					if r.URL.Query().Get("after_id") != "claude-newer-sonnet" {
						t.Error("wrong cursor")
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"claude-older-sonnet","created_at":"2025-01-01T00:00:00Z"}],"has_more":false}`))
				}
			}))
			defer upstream.Close()
			s, repo := fixture(t, testEngine{fail: true}, upstream.URL)
			enablePublic(s)
			w := modelRequest(s, provider, "synthetic-list-key")
			var got struct {
				Data []providerModel `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Data) != 2 || !strings.Contains(got.Data[0].ID, "newer") {
				t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "must-not-return") || strings.Contains(w.Body.String(), "synthetic-list-key") {
				t.Fatal("unnecessary data returned or cached")
			}
			events, err := repo.Events(context.Background(), "org-a", 100, 0)
			if err != nil || len(events) != 0 {
				t.Fatal("discovery wrote audit events")
			}
			want := int32(1)
			if provider == "anthropic" {
				want = 2
			}
			if calls.Load() != want {
				t.Fatal("unexpected number of provider requests")
			}
		})
	}
}

func TestModelsRejectInvalidInputsBeforeDispatch(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	for _, tc := range []struct {
		provider string
		keys     []string
		status   int
	}{
		{"openai", nil, 401},
		{"openai", []string{"bad key"}, 400},
		{"openai", []string{"one", "two"}, 400},
		{"https://attacker.test", []string{"synthetic-list-key"}, 400},
	} {
		if w := modelRequest(s, tc.provider, tc.keys...); w.Code != tc.status {
			t.Fatalf("status %d want %d", w.Code, tc.status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input dispatched or server credential used")
	}
}

func TestModelsBoundedResponsesAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body           string
		upstreamStatus, want int
		code                 string
	}{
		{"bad key", `{"key":"synthetic-list-key"}`, 401, 401, "provider_key_rejected"},
		{"permissions", `private details`, 403, 403, "models_forbidden"},
		{"rate", `private details`, 429, 429, "rate_limited"},
		{"failure", `private details`, 500, 502, "models_unavailable"},
		{"malformed", `{"data":`, 200, 502, "models_unavailable"},
		{"missing", `{}`, 200, 502, "models_unavailable"},
		{"oversized", strings.Repeat(" ", 2*1024*1024+1), 200, 502, "models_unavailable"},
		{"repeated cursor", `{"data":[],"has_more":true,"last_id":"same"}`, 200, 502, "models_unavailable"},
		{"empty", `{"data":[]}`, 200, 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.upstreamStatus)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			s, _ := fixture(t, testEngine{}, upstream.URL)
			enablePublic(s)
			w := modelRequest(s, "anthropic", "synthetic-list-key")
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private details") || strings.Contains(w.Body.String(), "synthetic-list-key") {
				t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestModelsDoNotFollowRedirectsAndApplyRateLimit(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	s.config.PublicRPM = 1
	if w := modelRequest(s, "openai", "synthetic-key"); w.Code != 502 {
		t.Fatal(w.Code)
	}
	if w := modelRequest(s, "openai", "synthetic-key"); w.Code != 429 {
		t.Fatal(w.Code)
	}
	if redirected.Load() != 0 {
		t.Fatal("credential followed redirect")
	}
}

func TestModelsPropagateCancellation(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-time.After(3 * time.Second):
		}
	}))
	defer upstream.Close()
	s, _ := fixture(t, testEngine{}, upstream.URL)
	enablePublic(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "/public/v1/models", strings.NewReader(`{"provider":"openai"}`)).WithContext(ctx)
	r.Header.Set("X-Provider-API-Key", "synthetic-key")
	done := make(chan struct{})
	go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no upstream request")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation not propagated")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not stop")
	}
}
