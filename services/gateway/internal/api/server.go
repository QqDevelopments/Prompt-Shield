package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/ratelimit"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/workqueue"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const consoleThemeScriptHash = "'sha256-jjAGtpEC03TrmJWyiu1M1HaOlNSnuNDLLHSRYUt295g='"

type Engine interface {
	Inspect(context.Context, *pb.SanitizeRequest) (*pb.SanitizeResponse, error)
	Restore(context.Context, *pb.DeAnonymizeRequest) (*pb.DeAnonymizeResponse, error)
	Ready(context.Context) error
}

func (s *Server) contextualReady(ctx context.Context) bool {
	capability, ok := s.engine.(interface{ ContextualReady(context.Context) bool })
	return ok && capability.ContextualReady(ctx)
}

type Config struct {
	Queue               *workqueue.Scheduler
	DisableIPRateLimits bool
	TrustedProxies      []netip.Prefix
	Hosted              bool
	PublicEnabled       bool
	PublicHMACKey       string
	PublicDailyPerIP    int
	PublicRPM           int
	PublicPerIP         int
	PublicGlobal        int
	AllowedOrigins      []string
	DailyTokenBudget    int
	MetricsToken        string
	Organization        string
	PasswordHash        []byte
	Origin              string
	SecureCookies       bool
	Assets              string
	OpenAIURL           string
	OpenAIKey           string
	AnthropicURL        string
	AnthropicKey        string
	GeminiURL           string
	GeminiKey           string
	EngineTimeout       time.Duration
	UpstreamTimeout     time.Duration
}
type session struct {
	csrf    string
	expires time.Time
}
type Server struct {
	metrics  metrics
	config   Config
	repo     store.Repository
	limiter  ratelimit.Limiter
	engine   Engine
	client   *http.Client
	mu       sync.Mutex
	sessions map[string]session
}

func New(config Config, repo store.Repository, limiter ratelimit.Limiter, engine Engine) *Server {
	if config.Queue == nil {
		config.Queue = workqueue.New(workqueue.NewMemory(), workqueue.Defaults())
	}
	if config.GeminiURL == "" {
		config.GeminiURL = DefaultGeminiURL
	}
	if config.PublicRPM <= 0 {
		config.PublicRPM = 10
	}
	if config.PublicPerIP <= 0 {
		config.PublicPerIP = 2
	}
	if config.PublicGlobal <= 0 {
		config.PublicGlobal = 20
	}
	if config.DailyTokenBudget <= 0 {
		config.DailyTokenBudget = 1000000
	}
	if config.EngineTimeout <= 0 {
		config.EngineTimeout = 500 * time.Millisecond
	}
	if config.UpstreamTimeout <= 0 {
		config.UpstreamTimeout = 60 * time.Second
	}
	return &Server{config: config, repo: repo, limiter: limiter, engine: engine, sessions: map[string]session{}, client: &http.Client{Timeout: config.UpstreamTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "type": "prompt_shield_error", "message": code}})
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid_json")
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /metrics", s.serveMetrics)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/session", s.sessionInfo)
	mux.Handle("POST /api/logout", s.console(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/overview", s.console(http.HandlerFunc(s.overview)))
	mux.Handle("GET /api/policy", s.console(http.HandlerFunc(s.getPolicy)))
	mux.Handle("PUT /api/policy", s.console(http.HandlerFunc(s.savePolicy)))
	mux.Handle("GET /api/keys", s.console(http.HandlerFunc(s.keys)))
	mux.Handle("POST /api/keys", s.console(http.HandlerFunc(s.createKey)))
	mux.Handle("DELETE /api/keys/{id}", s.console(http.HandlerFunc(s.revokeKey)))
	mux.Handle("GET /api/events", s.console(http.HandlerFunc(s.events)))
	mux.Handle("POST /api/inspect", s.console(http.HandlerFunc(s.inspect)))
	mux.Handle("POST /v1/chat/completions", s.apiKey(http.HandlerFunc(s.proxy)))
	mux.Handle("POST /v1/gemini/chat/completions", s.apiKey(http.HandlerFunc(s.proxy)))
	mux.Handle("POST /v1/messages", s.apiKey(http.HandlerFunc(s.proxy)))
	mux.Handle("POST /v1/embeddings", s.apiKey(http.HandlerFunc(s.proxy)))
	mux.Handle("POST /v1/inspect", s.apiKey(http.HandlerFunc(s.inspect)))
	if s.config.PublicEnabled {
		mux.Handle("POST /public/v1/models", s.public(http.HandlerFunc(s.models)))
		mux.Handle("POST /public/v1/inspect", s.public(http.HandlerFunc(s.inspect)))
		mux.Handle("POST /public/v1/chat/completions", s.public(http.HandlerFunc(s.proxy)))
		mux.Handle("POST /public/v1/gemini/chat/completions", s.public(http.HandlerFunc(s.proxy)))
		mux.Handle("POST /public/v1/messages", s.public(http.HandlerFunc(s.proxy)))
	}
	mux.HandleFunc("/", s.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if !s.cors(w, r) {
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' "+consoleThemeScriptHash+"; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		ctx, cancel := context.WithTimeout(r.Context(), s.config.UpstreamTimeout+5*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) assets(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/public/") {
		fail(w, 404, "not_found")
		return
	}
	clean := filepath.Clean(filepath.FromSlash("/" + r.URL.Path))
	path := filepath.Join(s.config.Assets, strings.TrimLeft(clean, "/\\"))
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		path = filepath.Join(s.config.Assets, "index.html")
	}
	if _, err := os.Stat(path); err != nil {
		fail(w, 503, "console_not_built")
		return
	}
	http.ServeFile(w, r, path)
}
func (s *Server) sameOrigin(r *http.Request) bool { return s.allowedOrigin(r.Header.Get("Origin")) }
func (s *Server) getSession(r *http.Request) (session, bool) {
	cookie, err := r.Cookie("ps_session")
	if err != nil {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, value := range s.sessions {
		if now.After(value.expires) {
			delete(s.sessions, key)
		}
	}
	value, ok := s.sessions[store.Hash(cookie.Value)]
	return value, ok
}
func (s *Server) console(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := s.getSession(r)
		if !ok {
			fail(w, 401, "unauthorized")
			return
		}
		if r.Method != "GET" && (!s.sameOrigin(r) || subtle.ConstantTimeCompare([]byte(value.csrf), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
			fail(w, 403, "csrf_invalid")
			return
		}
		if !s.limit(w, r, "console:"+s.config.Organization, 120) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) limit(w http.ResponseWriter, r *http.Request, key string, limit int) bool {
	allowed, err := s.limiter.Allow(r.Context(), key, limit)
	if err != nil {
		fail(w, 503, "rate_limit_unavailable")
		return false
	}
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	if !allowed {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "rate_limited")
		return false
	}
	return true
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		fail(w, 403, "csrf_invalid")
		return
	}
	host, valid := s.clientIP(r)
	if !valid {
		fail(w, 400, "invalid_client_address")
		return
	}
	if !s.limit(w, r, "login:"+host, 10) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if decode(w, r, &body) != nil || len(body.Password) > 72 {
		fail(w, 400, "invalid_request")
		return
	}
	if bcrypt.CompareHashAndPassword(s.config.PasswordHash, []byte(body.Password)) != nil {
		fail(w, 401, "unauthorized")
		return
	}
	token := store.Token()
	value := session{csrf: store.Token(), expires: time.Now().Add(8 * time.Hour)}
	s.mu.Lock()
	for key, v := range s.sessions {
		if time.Now().After(v.expires) {
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		fail(w, 503, "session_limit")
		return
	}
	s.sessions[store.Hash(token)] = value
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "ps_session", Value: token, Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	respond(w, 200, map[string]string{"csrf_token": value.csrf, "organization": s.config.Organization})
}
func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	value, ok := s.getSession(r)
	if !ok {
		fail(w, 401, "unauthorized")
		return
	}
	respond(w, 200, map[string]string{"csrf_token": value.csrf, "organization": s.config.Organization})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("ps_session"); err == nil {
		s.mu.Lock()
		delete(s.sessions, store.Hash(cookie.Value))
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "ps_session", Value: "", Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	respond(w, 200, map[string]bool{"ok": true})
}

type orgKey struct{}

func (s *Server) apiKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}
		if r.URL.Path == "/v1/messages" && r.Header.Get("x-api-key") != "" {
			token = r.Header.Get("x-api-key")
		}
		if !strings.HasPrefix(token, "ps_") || len(token) > 200 {
			fail(w, 401, "unauthorized")
			return
		}
		org, err := s.repo.Authenticate(r.Context(), store.Hash(token))
		if err != nil {
			fail(w, 401, "unauthorized")
			return
		}
		if !s.limit(w, r, "org:"+org, 120) {
			return
		}
		if !s.limit(w, r, "key:"+store.Hash(token), 120) {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), orgKey{}, org)))
	})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if s.engine.Ready(ctx) != nil || s.repo.Ready(ctx) != nil || s.limiter.Ready(ctx) != nil || s.config.Queue.Ready(ctx) != nil {
		fail(w, 503, "dependency_unavailable")
		return
	}
	mode := "rules-only"
	if s.contextualReady(ctx) {
		mode = "hybrid"
	}
	respond(w, 200, map[string]string{"status": "ready", "mode": mode})
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	events, err := s.repo.Events(ctx, s.config.Organization, 100, 0)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	counts := map[string]int{"allowed": 0, "masked": 0, "blocked": 0, "failed": 0}
	for _, e := range events {
		counts[e.Decision]++
	}
	contextual := s.contextualReady(ctx)
	mode := "rules-only"
	if contextual {
		mode = "hybrid"
	}
	respond(w, 200, map[string]any{"engine_ready": s.engine.Ready(ctx) == nil, "mode": mode, "contextual_ready": contextual, "model_languages": []string{"en"}, "counts": counts, "sample_size": len(events), "sample_limit": 100, "openai_configured": s.config.OpenAIKey != "", "anthropic_configured": s.config.AnthropicKey != "", "gemini_configured": s.config.GeminiKey != "", "base_url": s.config.Origin + "/v1"})
}
func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := s.repo.Policy(r.Context(), s.config.Organization)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	respond(w, 200, p)
}
func (s *Server) savePolicy(w http.ResponseWriter, r *http.Request) {
	var p store.Policy
	if decode(w, r, &p) != nil || len(p.BlockedKeywords) > 64 {
		fail(w, 400, "invalid_policy")
		return
	}
	if !p.BlockSecrets {
		fail(w, 400, "critical_blocking_required")
		return
	}
	for _, keyword := range p.BlockedKeywords {
		if strings.TrimSpace(keyword) == "" || len(keyword) > 128 {
			fail(w, 400, "invalid_policy")
			return
		}
	}
	if p.ContextualPII && !s.contextualReady(r.Context()) {
		fail(w, 422, "contextual_model_unavailable")
		return
	}
	if err := s.repo.SavePolicy(r.Context(), s.config.Organization, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			fail(w, 409, "policy_conflict")
			return
		}
		fail(w, 503, "storage_unavailable")
		return
	}
	p.Version++
	respond(w, 200, p)
}
func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.repo.Keys(r.Context(), s.config.Organization)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	respond(w, 200, keys)
}
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if decode(w, r, &body) != nil || len(strings.TrimSpace(body.Name)) == 0 || len(body.Name) > 80 {
		fail(w, 400, "invalid_request")
		return
	}
	secret := "ps_" + store.Token()
	key := store.Key{ID: store.Token(), Name: strings.TrimSpace(body.Name), Prefix: secret[:12], Hash: store.Hash(secret), Active: true, CreatedAt: time.Now().UTC()}
	if err := s.repo.AddKey(r.Context(), s.config.Organization, key); err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	respond(w, 201, map[string]any{"key": key, "secret": secret})
}
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.RevokeKey(r.Context(), s.config.Organization, r.PathValue("id")); err != nil {
		fail(w, 404, "not_found")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 100000 {
		fail(w, 400, "invalid_request")
		return
	}
	decision := r.URL.Query().Get("decision")
	if decision != "" && decision != "allowed" && decision != "masked" && decision != "blocked" && decision != "failed" {
		fail(w, 400, "invalid_request")
		return
	}
	events, err := s.repo.Events(r.Context(), s.config.Organization, 50, offset, decision)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	respond(w, 200, events)
}
func (s *Server) scan(ctx context.Context, org, id, text string, policy store.Policy) (*pb.SanitizeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.EngineTimeout)
	defer cancel()
	started := time.Now()
	result, err := s.engine.Inspect(ctx, &pb.SanitizeRequest{RequestId: id, OrganizationId: org, Text: text, Policy: &pb.SecurityPolicy{MaskPii: policy.MaskPII, BlockSecrets: policy.BlockSecrets, ContextualPii: policy.ContextualPII, BlockedKeywords: policy.BlockedKeywords}})
	if err != nil {
		auditContext, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		_ = s.audit(auditContext, org, id, &pb.SanitizeResponse{ReasonCode: "failed"}, started)
	}
	return result, err
}

func failInspection(w http.ResponseWriter, err error) {
	if status.Code(err) == codes.ResourceExhausted {
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusServiceUnavailable, "inspection_busy")
		return
	}
	fail(w, http.StatusServiceUnavailable, "inspection_unavailable")
}

func (s *Server) audit(ctx context.Context, org, id string, result *pb.SanitizeResponse, started time.Time) error {
	decision := result.ReasonCode
	if result.IsBlocked {
		decision = "blocked"
	}
	err := s.repo.AddEvent(ctx, org, store.Event{ID: id, Decision: decision, Reason: result.ReasonCode, EntityCount: result.EntityCount, EntityTypes: append([]string{}, result.EntityTypes...), LatencyMS: time.Since(started).Milliseconds(), CreatedAt: time.Now().UTC()})
	if err != nil {
		s.metrics.auditFailures.Add(1)
	} else {
		s.recordDecision(decision, time.Since(started))
	}
	return err
}
func (s *Server) inspect(w http.ResponseWriter, r *http.Request) {
	if !validProgress(w, r) {
		return
	}
	org := s.config.Organization
	if authenticated, ok := r.Context().Value(orgKey{}).(string); ok {
		org = authenticated
	}
	var body struct {
		Text string `json:"text"`
	}
	if decode(w, r, &body) != nil || len(body.Text) > 128*1024 {
		fail(w, 400, "invalid_request")
		return
	}
	p, err := s.repo.Policy(r.Context(), org)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	w, r, finish, admitted := s.work(w, r)
	if !admitted {
		return
	}
	defer finish()
	p, err = s.repo.Policy(r.Context(), org)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	started := time.Now()
	id := store.Token()
	result, err := s.scan(r.Context(), org, id, body.Text, p)
	if err != nil {
		failInspection(w, err)
		return
	}
	defer func() {
		for _, mapping := range result.Mappings {
			mapping.Original = ""
			mapping.Placeholder = ""
		}
		clear(result.Mappings)
	}()
	if err = s.audit(r.Context(), org, id, result, started); err != nil {
		fail(w, 503, "audit_unavailable")
		return
	}
	respond(w, 200, map[string]any{"sanitized_text": result.SanitizedText, "is_blocked": result.IsBlocked, "reason_code": result.ReasonCode, "entity_types": result.EntityTypes, "entity_count": result.EntityCount, "processing_time_micros": result.ProcessingTimeMicros})
}

func ValidateURL(raw string, allowLocal bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid_provider_url")
	}
	if u.Scheme != "https" && !(allowLocal && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")) {
		return errors.New("provider_requires_https")
	}
	return nil
}
