package api

import (
	"bytes"
	"context"
	"errors"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConsumedBufferIsClearedBeforeReuse(t *testing.T) {
	buffer := new(bytes.Buffer)
	buffer.WriteString("synthetic-sensitive-buffer")
	backing := buffer.Bytes()
	_, _ = io.ReadAll(buffer)
	release(buffer)
	for _, value := range backing {
		if value != 0 {
			t.Fatal("consumed buffer retained content")
		}
	}
}

func TestProviderKeyHeaderContracts(t *testing.T) {
	s, repo := fixture(t, testEngine{}, "https://provider.test/v1")
	key := "ps_header-fixture"
	if err := repo.AddKey(context.Background(), "org-a", store.Key{ID: "headers", Hash: store.Hash(key), Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, authorization, xkey string
		want                      int
	}{
		{"/v1/chat/completions", "Bearer " + key, "", 204},
		{"/v1/chat/completions", key, "", 401},
		{"/v1/messages", "Bearer " + key, "", 204},
		{"/v1/messages", "", key, 204},
		{"/v1/messages", "Bearer " + key, "ps_invalid", 401},
	} {
		r := httptest.NewRequest("POST", test.path, nil)
		r.Header.Set("Authorization", test.authorization)
		r.Header.Set("x-api-key", test.xkey)
		w := httptest.NewRecorder()
		s.apiKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("header contract for %s: got %d want %d", test.path, w.Code, test.want)
		}
	}
}

type unavailableAudit struct{ store.Repository }

func (unavailableAudit) AddEvent(context.Context, string, store.Event) error {
	return errors.New("offline")
}
func TestAuditFailureNeverDispatches(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer upstream.Close()
	s, repo := fixture(t, testEngine{}, upstream.URL)
	_ = repo.AddKey(context.Background(), "org-a", store.Key{ID: "fixture", Hash: store.Hash("ps_fixture"), Active: true})
	s.repo = unavailableAudit{repo}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`))
	r.Header.Set("Authorization", "Bearer ps_fixture")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 503 || calls != 0 {
		t.Fatal("audit failure did not close egress")
	}
}
func TestUpstreamCancellationPropagates(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/gemini/chat/completions"} {
		t.Run(path, func(t *testing.T) { upstreamCancellationPropagates(t, path) })
	}
}

func upstreamCancellationPropagates(t *testing.T, path string) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	s, repo := fixture(t, testEngine{}, upstream.URL)
	_ = repo.AddKey(context.Background(), "org-a", store.Key{ID: "fixture", Hash: store.Hash("ps_fixture"), Active: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"hello"}]}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer ps_fixture")
	done := make(chan struct{})
	go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not entered")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not cancel")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler retained request")
	}
}
func TestPolicyConflictAtHTTPBoundary(t *testing.T) {
	s, _ := fixture(t, testEngine{}, "https://provider.test/v1")
	cookie, csrf := loginFixture(t, s)
	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/api/policy", strings.NewReader(`{"version":1,"mask_pii":true,"block_secrets":true,"contextual_pii":false,"blocked_keywords":[]}`))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "http://console.test")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if send().Code != 200 {
		t.Fatal("initial save failed")
	}
	if send().Code != 409 {
		t.Fatal("stale policy overwrote newer revision")
	}
}
