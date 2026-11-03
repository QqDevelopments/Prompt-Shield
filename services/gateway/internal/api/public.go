package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

func validProviderKey(key string) bool {
	if len(key) == 0 || len(key) > 4096 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func (s *Server) allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == s.config.Origin {
		return true
	}
	for _, allowed := range s.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) cors(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	w.Header().Add("Vary", "Origin")
	if origin != "" {
		if !s.allowedOrigin(origin) {
			fail(w, 403, "origin_not_allowed")
			return false
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After, X-RateLimit-Limit")
	}
	if r.Method != http.MethodOptions {
		return true
	}
	if origin == "" {
		fail(w, 403, "origin_not_allowed")
		return false
	}
	method := r.Header.Get("Access-Control-Request-Method")
	if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" {
		fail(w, 403, "cors_method_denied")
		return false
	}
	for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "", "content-type", "authorization", "x-api-key", "x-csrf-token", "x-provider-api-key", "x-prompt-shield-progress":
		default:
			fail(w, 403, "cors_header_denied")
			return false
		}
	}
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-CSRF-Token, X-Provider-API-Key, X-Prompt-Shield-Progress")
	w.WriteHeader(http.StatusNoContent)
	return false
}

func (s *Server) clientIP(r *http.Request) (string, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", false
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "", false
	}
	peer = peer.Unmap()
	trusted := func(addr netip.Addr) bool {
		for _, prefix := range s.config.TrustedProxies {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String(), true
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(chain) > 32 {
		return "", false
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if !trusted(peer) {
			break
		}
		next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			return "", false
		}
		peer = next.Unmap()
	}
	return peer.String(), true
}

// Walk only the explicitly trusted proxy suffix; a direct client cannot choose its identity.
func (s *Server) public(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.config.PublicHMACKey) < 32 {
			fail(w, 503, "public_not_configured")
			return
		}
		host, valid := s.clientIP(r)
		if !valid {
			fail(w, 400, "invalid_client_address")
			return
		}
		mac := hmac.New(sha256.New, []byte(s.config.PublicHMACKey))
		_, _ = mac.Write([]byte(host))
		id := hex.EncodeToString(mac.Sum(nil))
		if s.config.PublicDailyPerIP > 0 {
			budget, ok := s.limiter.(interface {
				Reserve(context.Context, string, int, int) (bool, error)
			})
			if !ok {
				fail(w, 503, "rate_limit_unavailable")
				return
			}
			allowed, err := budget.Reserve(r.Context(), "public-request:"+id, 1, s.config.PublicDailyPerIP)
			if err != nil {
				fail(w, 503, "rate_limit_unavailable")
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.config.PublicDailyPerIP))
			if !allowed {
				now := time.Now().UTC()
				reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(reset.Sub(now).Seconds()))))
				fail(w, 429, "daily_limit_exceeded")
				return
			}
		}
		if !s.config.DisableIPRateLimits && !s.limit(w, r, "public:"+id, s.config.PublicRPM) {
			return
		}
		leases, ok := s.limiter.(interface {
			Acquire(context.Context, string, string, int, int, time.Duration) (bool, error)
			Release(context.Context, string, string) error
		})
		if !ok {
			fail(w, 503, "rate_limit_unavailable")
			return
		}
		lease := store.Token()
		perIP := s.config.PublicPerIP
		if s.config.DisableIPRateLimits {
			perIP = 0
		}
		allowed, err := leases.Acquire(r.Context(), id, lease, perIP, s.config.PublicGlobal, s.config.Queue.Settings.Wait+s.config.Queue.Settings.Processing+10*time.Second)
		if err != nil {
			fail(w, 503, "rate_limit_unavailable")
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(1))
			fail(w, 429, "concurrency_limited")
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = leases.Release(ctx, id, lease)
		}()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), orgKey{}, s.config.Organization)))
	})
}
