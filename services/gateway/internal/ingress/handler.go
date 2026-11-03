package ingress

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Upstream       *url.URL
	Origin         string
	Product        string
	HMACKey        string
	MetricsToken   string
	TrustedProxies []netip.Prefix
	DailyEnabled   bool
	// Now is optional and permits deterministic UTC rollover verification.
	Now func() time.Time
}

type Handler struct {
	cfg         Config
	counter     Counter
	proxy       *httputil.ReverseProxy
	mu          sync.Mutex
	exhausted   map[string]struct{}
	day         string
	forwarded   atomic.Uint64
	denied      atomic.Uint64
	preflights  atomic.Uint64
	storeErrors atomic.Uint64
}

func New(cfg Config, counter Counter) (*Handler, error) {
	validOrigin := func(u *url.URL) bool {
		return u != nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
	}
	origin, err := url.Parse(cfg.Origin)
	if err != nil || !validOrigin(origin) || origin.Scheme != "https" || cfg.Origin != origin.Scheme+"://"+origin.Host {
		return nil, errors.New("invalid_ingress_origin")
	}
	if !validOrigin(cfg.Upstream) || (cfg.Upstream.Scheme != "http" && cfg.Upstream.Scheme != "https") || cfg.Upstream.Opaque != "" || len(cfg.HMACKey) < 32 || counter == nil {
		return nil, errors.New("invalid_ingress_configuration")
	}
	if cfg.Product != "community" && cfg.Product != "enterprise" {
		return nil, errors.New("invalid_ingress_product")
	}
	if len(cfg.MetricsToken) < 32 {
		return nil, errors.New("invalid_ingress_metrics_token")
	}
	for _, prefix := range cfg.TrustedProxies {
		if !prefix.IsValid() || prefix.Bits() == 0 {
			return nil, errors.New("invalid_ingress_proxy")
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	h := &Handler{cfg: cfg, counter: counter, exhausted: make(map[string]struct{})}
	h.proxy = &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(cfg.Upstream)
			// Remove all client-supplied forwarding metadata. The application trusts
			// only this ingress peer and receives exactly one verified client IP.
			p.Out.Header.Del("Forwarded")
			p.Out.Header.Del("X-Real-IP")
			p.Out.Header.Del("X-Forwarded-For")
			p.Out.Header.Del("X-Forwarded-Host")
			p.Out.Header.Del("X-Forwarded-Proto")
			ip, _ := h.clientIP(p.In)
			p.Out.Header.Set("X-Forwarded-For", ip.String())
			p.Out.Header.Set("X-Forwarded-Proto", "https")
		},
		Transport:     &http.Transport{Proxy: nil, MaxIdleConns: 100, MaxIdleConnsPerHost: 20, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 395 * time.Second, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext},
		FlushInterval: -1,
		ModifyResponse: func(r *http.Response) error {
			// These are already set once by the edge, including on edge errors.
			r.Header.Del("Access-Control-Allow-Origin")
			r.Header.Del("Access-Control-Allow-Credentials")
			r.Header.Del("Access-Control-Expose-Headers")
			if cfg.DailyEnabled && r.Request.Method == "POST" && h.publicAction(r.Request.URL.Path) {
				r.Header.Del("X-RateLimit-Limit")
			}
			return nil
		},
		ErrorLog:     log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) { fail(w, 502, "gateway_unavailable") },
	}
	return h, nil
}

func fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": code, "type": "prompt_shield_error"}})
}

func (h *Handler) clientIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range h.cfg.TrustedProxies {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer, true
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(chain) > 32 {
		return netip.Addr{}, false
	}
	for i := len(chain) - 1; i >= 0 && trusted(peer); i-- {
		peer, err = netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		peer = peer.Unmap()
	}
	return peer, true
}

func (h *Handler) publicAction(path string) bool {
	if h.cfg.Product == "enterprise" {
		return path == "/public/v1/applications" || path == "/public/v1/application-access" || path == "/public/v1/application-access/consume"
	}
	switch path {
	case "/public/v1/models", "/public/v1/inspect", "/public/v1/chat/completions", "/public/v1/messages", "/public/v1/gemini/chat/completions":
		return true
	}
	return false
}

func (h *Handler) cached(key, day string, mark bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.day != day {
		clear(h.exhausted)
		h.day = day
	}
	_, found := h.exhausted[key]
	// Redis remains authoritative when this bounded acceleration cache is full.
	if mark && len(h.exhausted) < 100000 {
		h.exhausted[key] = struct{}{}
	}
	return found
}

func (h *Handler) quotaDenied(w http.ResponseWriter, now, reset time.Time) {
	h.denied.Add(1)
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(reset.Sub(now).Seconds()))))
	code := "daily_limit_exceeded"
	if h.cfg.Product == "enterprise" {
		code = "application_daily_limit_exceeded"
	}
	fail(w, 429, code)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/ingress/healthz" {
		w.WriteHeader(200)
		return
	}
	if r.URL.Path == "/ingress/metrics" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.cfg.MetricsToken)) != 1 {
			fail(w, 401, "unauthorized")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]uint64{"forwarded": h.forwarded.Load(), "daily_denied": h.denied.Load(), "preflights": h.preflights.Load(), "store_errors": h.storeErrors.Load()})
		return
	}
	// URL.Path is decoded; disallow alternative encodings/path normalization to
	// keep admission and the Go ServeMux routing boundary identical.
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") || strings.Contains(r.URL.Path, "/./") || strings.Contains(r.URL.Path, "/../") || strings.HasSuffix(r.URL.Path, "/.") || strings.HasSuffix(r.URL.Path, "/..") {
		fail(w, 400, "invalid_path")
		return
	}
	origin := r.Header.Get("Origin")
	w.Header().Add("Vary", "Origin")
	if origin != "" {
		if origin != h.cfg.Origin {
			fail(w, 403, "origin_not_allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After, X-RateLimit-Limit")
	}
	ip, valid := h.clientIP(r)
	if !valid {
		fail(w, 400, "invalid_client_address")
		return
	}
	now := h.cfg.Now().UTC()
	day := now.Format("2006-01-02")
	reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	mac := hmac.New(sha256.New, []byte(h.cfg.HMACKey))
	_, _ = mac.Write([]byte(ip.String()))
	key := "ps:ingress:" + h.cfg.Product + ":" + day + ":" + hex.EncodeToString(mac.Sum(nil))
	limited := h.cfg.DailyEnabled && h.publicAction(r.URL.Path) && (r.Method == "POST" || r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") == "POST")
	if limited {
		w.Header().Set("X-RateLimit-Limit", "5")
		if h.cached(key, day, false) {
			h.quotaDenied(w, now, reset)
			return
		}
	}
	if r.Method == "OPTIONS" {
		if origin == "" {
			fail(w, 403, "origin_not_allowed")
			return
		}
		method := r.Header.Get("Access-Control-Request-Method")
		if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" && !(method == "PATCH" && h.cfg.Product == "enterprise") {
			fail(w, 403, "cors_method_denied")
			return
		}
		for _, value := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "", "content-type", "authorization", "x-api-key", "x-csrf-token", "x-provider-api-key", "x-prompt-shield-progress":
			case "x-organization-context":
				if h.cfg.Product != "enterprise" {
					fail(w, 403, "cors_header_denied")
					return
				}
			default:
				fail(w, 403, "cors_header_denied")
				return
			}
		}
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-CSRF-Token, X-Provider-API-Key, X-Prompt-Shield-Progress")
		if h.cfg.Product == "enterprise" {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-Provider-API-Key, X-Organization-Context, X-Prompt-Shield-Progress")
		}
		h.preflights.Add(1)
		w.WriteHeader(204)
		return
	}
	if limited {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		used, err := h.counter.Admit(ctx, key, reset, 5)
		cancel()
		if err != nil || used < 1 {
			h.storeErrors.Add(1)
			fail(w, 503, "rate_limit_unavailable")
			return
		}
		if used >= 5 {
			h.cached(key, day, true)
		}
		if used > 5 {
			h.quotaDenied(w, now, reset)
			return
		}
	}
	h.forwarded.Add(1)
	h.proxy.ServeHTTP(w, r)
}
