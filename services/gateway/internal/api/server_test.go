package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	realengine "github.com/qqdevelopments/prompt-shield/services/gateway/internal/engine"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/ratelimit"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testEngine struct{ fail bool }

func (e testEngine) Ready(context.Context) error { return nil }
func (e testEngine) Inspect(ctx context.Context, in *pb.SanitizeRequest) (*pb.SanitizeResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("deadline_required")
	}
	if e.fail {
		return nil, errors.New("unavailable")
	}
	if strings.Contains(in.Text, "sk-fixture") {
		return &pb.SanitizeResponse{IsBlocked: true, ReasonCode: "critical_secret", EntityTypes: []string{"API_KEY"}, EntityCount: 1}, nil
	}
	if strings.Contains(in.Text, "alice@example.com") {
		return &pb.SanitizeResponse{SanitizedText: strings.ReplaceAll(in.Text, "alice@example.com", "[PS_test_EMAIL_1]"), ReasonCode: "masked", EntityCount: 1, EntityTypes: []string{"EMAIL"}, Mappings: []*pb.Mapping{{Placeholder: "[PS_test_EMAIL_1]", Original: "alice@example.com"}}}, nil
	}
	return &pb.SanitizeResponse{SanitizedText: in.Text, ReasonCode: "allowed"}, nil
}
func (e testEngine) Restore(ctx context.Context, in *pb.DeAnonymizeRequest) (*pb.DeAnonymizeResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("deadline_required")
	}
	text := in.Text
	for _, m := range in.Mappings {
		text = strings.ReplaceAll(text, m.Placeholder, m.Original)
	}
	return &pb.DeAnonymizeResponse{Text: text}, nil
}

func TestInspectionFailureDistinguishesCapacity(t *testing.T) {
	busy := httptest.NewRecorder()
	failInspection(busy, status.Error(codes.ResourceExhausted, "engine_busy"))
	if busy.Code != http.StatusServiceUnavailable || busy.Header().Get("Retry-After") != "1" || !strings.Contains(busy.Body.String(), "inspection_busy") {
		t.Fatalf("capacity response: status=%d retry=%q body=%s", busy.Code, busy.Header().Get("Retry-After"), busy.Body.String())
	}

	unavailable := httptest.NewRecorder()
	failInspection(unavailable, errors.New("unavailable"))
	if unavailable.Code != http.StatusServiceUnavailable || unavailable.Header().Get("Retry-After") != "" || !strings.Contains(unavailable.Body.String(), "inspection_unavailable") {
		t.Fatalf("unavailable response: status=%d retry=%q body=%s", unavailable.Code, unavailable.Header().Get("Retry-After"), unavailable.Body.String())
	}
}

func fixture(t *testing.T, engine Engine, upstream string) (*Server, *store.Memory) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := store.NewMemory()
	s := New(Config{Organization: "org-a", Origin: "http://console.test", PasswordHash: hash, OpenAIURL: upstream, OpenAIKey: "synthetic-provider-key", AnthropicURL: upstream, AnthropicKey: "synthetic-provider-key", GeminiURL: upstream, GeminiKey: "synthetic-provider-key"}, repo, ratelimit.NewMemory(), engine)
	return s, repo
}
func loginFixture(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"synthetic-test-password"}`))
	r.Header.Set("Origin", "http://console.test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("login %d", w.Code)
	}
	var session map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return w.Result().Cookies()[0], session["csrf_token"]
}

func TestLoginRateLimitUsesResolvedClientBehindTrustedProxy(t *testing.T) {
	s, _ := fixture(t, testEngine{}, "https://provider.test/v1")
	s.config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	login := func(forwarded, password string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"`+password+`"}`))
		request.RemoteAddr = "10.0.0.5:4567"
		request.Header.Set("Origin", s.config.Origin)
		request.Header.Set("X-Forwarded-For", forwarded)
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		return response
	}
	for range 10 {
		if response := login("198.51.100.10", "wrong-password"); response.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected failed-login response: %d", response.Code)
		}
	}
	if response := login("198.51.100.11", "synthetic-test-password"); response.Code != http.StatusOK {
		t.Fatalf("one client exhausted another client's login allowance: %d %s", response.Code, response.Body.String())
	}
}

func TestConsoleAuthenticationCSRFAndKeyRevocation(t *testing.T) {
	s, repo := fixture(t, testEngine{}, "https://provider.test/v1")
	cookie, csrf := loginFixture(t, s)
	for _, path := range []string{"/api/overview", "/api/events", "/api/keys", "/api/policy"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Errorf("%s exposed: %d", path, w.Code)
		}
	}
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "http://console.test")
		r.Header.Set("X-CSRF-Token", token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/keys", `{"name":"fixture"}`, ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	w := request("POST", "/api/keys", `{"name":"fixture"}`, csrf)
	if w.Code != 201 {
		t.Fatalf("create %d", w.Code)
	}
	var created struct {
		Key    store.Key `json:"key"`
		Secret string    `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	org, err := repo.Authenticate(context.Background(), store.Hash(created.Secret))
	if err != nil || org != "org-a" {
		t.Fatal("key not usable")
	}
	list := request("GET", "/api/keys", "", csrf)
	if strings.Contains(list.Body.String(), created.Secret) {
		t.Fatal("secret leaked in list")
	}
	if w = request("DELETE", "/api/keys/"+created.Key.ID, "", csrf); w.Code != 200 {
		t.Fatal("revoke failed")
	}
	if _, err = repo.Authenticate(context.Background(), store.Hash(created.Secret)); err == nil {
		t.Fatal("revoked key accepted")
	}
	if w = request("POST", "/api/logout", "", csrf); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w = request("GET", "/api/events", "", csrf); w.Code != 401 {
		t.Fatal("session survived logout")
	}
}

func TestProxyMasksRestoresAndNeverDispatchesBlockedOrFailedRequests(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-provider-key" {
			t.Error("wrong upstream credential")
		}
		var body proxyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(body.Messages[0].Content, "alice@example.com") {
			t.Error("raw value reached upstream")
		}
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": body.Messages[0].Content}}}})
	}))
	defer upstream.Close()
	s, repo := fixture(t, testEngine{}, upstream.URL)
	key := "ps_synthetic-api-key"
	if err := repo.AddKey(context.Background(), "org-a", store.Key{ID: "test-key", Hash: store.Hash(key), Active: true}); err != nil {
		t.Fatal(err)
	}
	send := func(text string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": []message{{Role: "user", Content: text}}})
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := send("Email alice@example.com"); w.Code != 200 || !strings.Contains(w.Body.String(), "alice@example.com") {
		t.Fatalf("round trip: %d", w.Code)
	}
	if w := send("sk-fixture"); w.Code != 400 {
		t.Fatalf("block: %d", w.Code)
	}
	if calls.Load() != 1 {
		t.Fatal("blocked request dispatched")
	}
	s.engine = testEngine{fail: true}
	if w := send("ordinary"); w.Code != 503 {
		t.Fatal("engine failure not closed")
	}
	if calls.Load() != 1 {
		t.Fatal("failed inspection dispatched")
	}
	events, err := repo.Events(context.Background(), "org-a", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "alice@example.com") || strings.Contains(string(encoded), "sk-fixture") {
		t.Fatal("audit leaked payload")
	}
}

func TestRealEngineHTTPBoundary(t *testing.T) {
	address := os.Getenv("ENGINE_TEST_ADDR")
	if address == "" {
		t.Skip("set ENGINE_TEST_ADDR and ENGINE_TOKEN for real Rust integration")
	}
	client, err := realengine.New(address, os.Getenv("ENGINE_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s, _ := fixture(t, client, "https://provider.test/v1")
	cookie, csrf := loginFixture(t, s)
	for _, test := range []struct {
		text    string
		blocked bool
		count   int
	}{{"hello alice@example.com", false, 1}, {"sk-abcdefghijklmnopqrstuvwx", true, 1}, {"hello world", false, 0}} {
		raw, _ := json.Marshal(map[string]string{"text": test.text})
		r := httptest.NewRequest("POST", "/api/inspect", strings.NewReader(string(raw)))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "http://console.test")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("real engine status %d", w.Code)
		}
		var result struct {
			Blocked bool `json:"is_blocked"`
			Count   int  `json:"entity_count"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Blocked != test.blocked || result.Count != test.count {
			t.Fatalf("unexpected result: %+v", result)
		}
		if strings.Contains(w.Body.String(), "alice@example.com") {
			t.Fatal("raw PII in inspection response")
		}
	}
}

func BenchmarkGatewayInspection(b *testing.B) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("synthetic-test-password"), bcrypt.MinCost)
	s := New(Config{Organization: "org-a", Origin: "http://console.test", PasswordHash: hash}, store.NewMemory(), ratelimit.NewMemory(), testEngine{})
	s.sessions[store.Hash("fixture-cookie")] = session{csrf: "fixture-csrf", expires: time.Now().Add(time.Hour)}
	handler := s.Handler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("POST", "/api/inspect", strings.NewReader(`{"text":"ordinary prompt"}`))
		r.AddCookie(&http.Cookie{Name: "ps_session", Value: "fixture-cookie"})
		r.Header.Set("Origin", "http://console.test")
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		s.limiter = ratelimit.NewMemory()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			b.Fatal(fmt.Sprint(w.Code))
		}
	}
}
