package ingress

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryCounter struct {
	mu     sync.Mutex
	counts map[string]int
	calls  int
	fail   bool
}

func (m *memoryCounter) Admit(_ context.Context, key string, _ time.Time, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.fail {
		return 0, errors.New("fixture unavailable")
	}
	if m.counts == nil {
		m.counts = map[string]int{}
	}
	if m.counts[key] >= limit {
		return limit + 1, nil
	}
	m.counts[key]++
	return m.counts[key], nil
}

func testIngress(t *testing.T, product string, store Counter, now func() time.Time, upstream http.Handler) *Handler {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Config{Upstream: u, Origin: "https://web.example.test", Product: product, HMACKey: strings.Repeat("h", 32), MetricsToken: strings.Repeat("m", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")}, DailyEnabled: true, Now: now}, store)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func call(h http.Handler, method, path, client string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(`{"text":"synthetic"}`))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", client)
	r.Header.Set("Origin", "https://web.example.test")
	if method == "OPTIONS" {
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "content-type,x-prompt-shield-progress")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDailyAdmissionAndPreflightNeverReachGatewayAfterFifth(t *testing.T) {
	store := &memoryCounter{}
	var forwarded atomic.Int32
	h := testIngress(t, "community", store, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		if r.Header.Get("X-Forwarded-For") != "198.51.100.7" {
			t.Error("verified client address not forwarded")
		}
		w.Header().Set("Access-Control-Allow-Origin", "https://web.example.test")
		w.WriteHeader(200)
	}))
	paths := []string{"/public/v1/models", "/public/v1/inspect", "/public/v1/chat/completions", "/public/v1/messages", "/public/v1/gemini/chat/completions"}
	for _, path := range paths {
		if w := call(h, "OPTIONS", path, "198.51.100.7"); w.Code != 204 {
			t.Fatalf("preflight: %d", w.Code)
		}
		w := call(h, "POST", path, "198.51.100.7")
		if w.Code != 200 || len(w.Header().Values("Access-Control-Allow-Origin")) != 1 {
			t.Fatalf("admitted response: %d %v", w.Code, w.Header())
		}
	}
	for _, method := range []string{"POST", "OPTIONS", "POST"} {
		w := call(h, method, paths[1], "198.51.100.7")
		if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "daily_limit_exceeded") {
			t.Fatalf("exhausted response: %d", w.Code)
		}
	}
	if forwarded.Load() != 5 || store.calls != 5 {
		t.Fatalf("forwarded=%d store=%d; cached rejection touched downstream", forwarded.Load(), store.calls)
	}
	for key := range store.counts {
		if strings.Contains(key, "198.51.100.7") {
			t.Fatal("plaintext IP stored")
		}
	}
}

func TestConcurrentIngressInstancesAndRestartUseSharedAdmission(t *testing.T) {
	store := &memoryCounter{}
	var forwarded atomic.Int32
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded.Add(1); w.WriteHeader(200) })
	a := testIngress(t, "community", store, nil, upstream)
	b := testIngress(t, "community", store, nil, upstream)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := a
			if i%2 == 1 {
				h = b
			}
			w := call(h, "POST", "/public/v1/inspect", "198.51.100.8")
			if w.Code != 200 && w.Code != 429 {
				t.Errorf("HTTP %d", w.Code)
			}
		}(i)
	}
	wg.Wait()
	if forwarded.Load() != 5 {
		t.Fatalf("forwarded %d", forwarded.Load())
	}
	restarted := testIngress(t, "community", store, nil, upstream)
	if w := call(restarted, "POST", "/public/v1/inspect", "198.51.100.8"); w.Code != 429 {
		t.Fatalf("restart bypass: %d", w.Code)
	}
}

func TestUTCRolloverFailureAndIndependentIPs(t *testing.T) {
	store := &memoryCounter{}
	now := time.Date(2026, 11, 2, 23, 59, 59, 0, time.UTC)
	h := testIngress(t, "community", store, func() time.Time { return now }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	for i := 0; i < 5; i++ {
		call(h, "POST", "/public/v1/inspect", "198.51.100.9")
	}
	if w := call(h, "POST", "/public/v1/inspect", "198.51.100.9"); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("UTC reset header missing")
	}
	if w := call(h, "POST", "/public/v1/inspect", "198.51.100.10"); w.Code != 200 {
		t.Fatal("IP quota not isolated")
	}
	now = now.Add(time.Second)
	if w := call(h, "POST", "/public/v1/inspect", "198.51.100.9"); w.Code != 200 {
		t.Fatal("UTC day did not reset")
	}
	store.fail = true
	if w := call(h, "POST", "/public/v1/inspect", "198.51.100.9"); w.Code != 503 {
		t.Fatal("counter outage failed open")
	}
}

func TestDailyDisabledAndEnterpriseScope(t *testing.T) {
	for _, product := range []string{"community", "enterprise"} {
		store := &memoryCounter{}
		h := testIngress(t, product, store, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
		path := "/public/v1/inspect"
		if product == "enterprise" {
			path = "/public/v1/applications"
		}
		for i := 0; i < 5; i++ {
			if w := call(h, "POST", path, "198.51.100.11"); w.Code != 200 {
				t.Fatal(w.Code)
			}
		}
		if w := call(h, "POST", path, "198.51.100.11"); w.Code != 429 {
			t.Fatal("quota missing")
		}
		if product == "enterprise" {
			if w := call(h, "POST", "/public/v1/application-access", "198.51.100.11"); w.Code != 429 {
				t.Fatal("application actions do not share quota")
			}
			for i := 0; i < 7; i++ {
				if w := call(h, "POST", "/api/chat/completions", "198.51.100.11"); w.Code != 200 {
					t.Fatal("authenticated chat inherited anonymous quota")
				}
			}
		}
		h.cfg.DailyEnabled = false
		for i := 0; i < 7; i++ {
			if w := call(h, "POST", path, "198.51.100.11"); w.Code != 200 {
				t.Fatal("explicit disable ignored")
			}
		}
		if store.calls != 5 {
			t.Fatal("disabled daily admission touched counter")
		}
	}
}

func TestEnterpriseConsumptionSharesAnonymousQuota(t *testing.T) {
	store := &memoryCounter{}
	var forwarded atomic.Int32
	h := testIngress(t, "enterprise", store, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	paths := []string{
		"/public/v1/applications",
		"/public/v1/application-access",
		"/public/v1/application-access/consume",
		"/public/v1/application-access",
		"/public/v1/application-access/consume",
	}
	for _, path := range paths {
		if w := call(h, http.MethodOptions, path, "198.51.100.21"); w.Code != http.StatusNoContent {
			t.Fatalf("preflight %s: HTTP %d", path, w.Code)
		}
		if w := call(h, http.MethodPost, path, "198.51.100.21"); w.Code != http.StatusAccepted {
			t.Fatalf("anonymous action %s: HTTP %d", path, w.Code)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		w := call(h, method, "/public/v1/application-access/consume", "198.51.100.21")
		if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "application_daily_limit_exceeded") {
			t.Fatalf("exhausted consumption %s: HTTP %d", method, w.Code)
		}
	}
	if forwarded.Load() != 5 || store.calls != 5 {
		t.Fatalf("exhausted consumption reached downstream: forwarded=%d, store=%d", forwarded.Load(), store.calls)
	}
}

func TestUntrustedHeadersCORSAndEncodedPaths(t *testing.T) {
	store := &memoryCounter{}
	h := testIngress(t, "community", store, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "198.51.100.12" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Real-IP") != "" {
			t.Error("forwarding headers not sanitized")
		}
		w.WriteHeader(200)
	}))
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/public/v1/inspect", nil)
		r.RemoteAddr = "198.51.100.12:12"
		r.Header.Set("X-Forwarded-For", "203.0.113."+strconv.Itoa(i))
		r.Header.Set("Forwarded", "for=203.0.113.5")
		r.Header.Set("X-Real-IP", "203.0.113.5")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (i < 5 && w.Code != 200) || (i == 5 && w.Code != 429) {
			t.Fatalf("spoofed identity bypass: %d", w.Code)
		}
	}
	for _, path := range []string{"/public/v1/%69nspect", "/public//v1/inspect", "/x/../public/v1/inspect"} {
		if w := call(h, "POST", path, "198.51.100.13"); w.Code != 400 {
			t.Fatalf("ambiguous path accepted %s", path)
		}
	}
	r := httptest.NewRequest("OPTIONS", "/public/v1/inspect", nil)
	r.RemoteAddr = "198.51.100.13:12"
	r.Header.Set("Origin", "https://web.example.test")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "x-untrusted")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("unapproved preflight header accepted")
	}
	r = httptest.NewRequest("POST", "/public/v1/inspect", nil)
	r.Header.Set("Origin", "https://attacker.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
}

func TestSSEFlushCancellationAndMetrics(t *testing.T) {
	cancelled := make(chan struct{})
	h := testIngress(t, "community", &memoryCounter{}, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: started\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	edge := httptest.NewServer(h)
	defer edge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", edge.URL+"/public/v1/inspect", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: started\n" {
		t.Fatalf("stream not flushed: %q %v", line, err)
	}
	_ = response.Body.Close()
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("upstream cancellation did not propagate")
	}
	metrics := httptest.NewRequest("GET", "/ingress/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, metrics)
	if w.Code != 401 {
		t.Fatal("metrics not protected")
	}
	metrics.Header.Set("Authorization", "Bearer "+strings.Repeat("m", 32))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, metrics)
	var counts map[string]uint64
	if err := json.Unmarshal(w.Body.Bytes(), &counts); err != nil || counts["forwarded"] != 1 {
		t.Fatal("missing metadata counters")
	}
}

func TestEnterpriseLoginRedirectCookiesAndOrganizationPreflight(t *testing.T) {
	store := &memoryCounter{}
	var forwarded atomic.Int32
	h := testIngress(t, "enterprise", store, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		switch r.URL.Path {
		case "/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "ps_login", Value: "synthetic", Path: "/auth", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, "https://identity.example.test/authorize?state=synthetic", http.StatusFound)
		case "/auth/callback":
			if r.URL.Query().Get("code") != "synthetic-code" || r.URL.Query().Get("state") != "synthetic-state" {
				t.Error("callback query changed")
			}
			http.SetCookie(w, &http.Cookie{Name: "ps_enterprise", Value: "synthetic-session", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "https://web.example.test", http.StatusSeeOther)
		case "/api/chat/completions":
			cookie, err := r.Cookie("ps_enterprise")
			if err != nil || cookie.Value != "synthetic-session" || r.Header.Get("X-Organization-Context") != "synthetic-org" {
				t.Error("session or organization header lost")
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	preflight := httptest.NewRequest("OPTIONS", "/api/chat/completions", nil)
	preflight.RemoteAddr = "198.51.100.20:1234"
	preflight.Header.Set("Origin", "https://web.example.test")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type,x-organization-context,x-prompt-shield-progress")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, preflight)
	if w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Organization-Context") || forwarded.Load() != 0 {
		t.Fatal("Enterprise preflight was not handled at ingress")
	}

	login := call(h, "GET", "/auth/login", "198.51.100.20")
	if login.Code != http.StatusFound || login.Header().Get("Location") != "https://identity.example.test/authorize?state=synthetic" || len(login.Result().Cookies()) != 1 || login.Result().Cookies()[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("OIDC login redirect or cookie changed")
	}
	callback := call(h, "GET", "/auth/callback?code=synthetic-code&state=synthetic-state", "198.51.100.20")
	if callback.Code != http.StatusSeeOther || callback.Header().Get("Location") != "https://web.example.test" || len(callback.Result().Cookies()) != 1 || callback.Result().Cookies()[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("OIDC callback redirect or cookie changed")
	}
	for i := 0; i < 7; i++ {
		r := httptest.NewRequest("POST", "/api/chat/completions", nil)
		r.RemoteAddr = "198.51.100.20:1234"
		r.Header.Set("Origin", "https://web.example.test")
		r.Header.Set("X-Organization-Context", "synthetic-org")
		r.AddCookie(&http.Cookie{Name: "ps_enterprise", Value: "synthetic-session"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("authenticated request %d: HTTP %d", i+1, w.Code)
		}
	}
	if store.calls != 0 {
		t.Fatal("identity or authenticated requests consumed anonymous daily allowance")
	}
}

func TestSlowUpstreamResponseHeaders(t *testing.T) {
	if os.Getenv("INGRESS_SLOW_TEST") != "1" {
		t.Skip("set INGRESS_SLOW_TEST=1 to verify a response after the previous 45-second timeout")
	}
	h := testIngress(t, "enterprise", &memoryCounter{}, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(46 * time.Second):
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/auth/login", nil).WithContext(ctx)
	r.RemoteAddr = "198.51.100.20:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("slow upstream response: HTTP %d", w.Code)
	}
}
