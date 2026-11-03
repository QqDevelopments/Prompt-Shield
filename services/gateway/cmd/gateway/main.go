package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/api"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/engine"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/ratelimit"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/workqueue"
	"github.com/qqdevelopments/prompt-shield/services/gateway/migrations"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func publicDailyLimit(development bool) int {
	if development {
		return 0
	}
	return 5
}

func publicLimitSettings(development bool, get func(string) string) (int, bool, error) {
	daily, err := workqueue.Bool(get, "PUBLIC_DAILY_IP_LIMIT_ENABLED", true)
	if err != nil {
		return 0, false, err
	}
	ip, err := workqueue.Bool(get, "PUBLIC_IP_RATE_LIMIT_ENABLED", true)
	if err != nil {
		return 0, false, err
	}
	limit := publicDailyLimit(development)
	if !daily {
		limit = 0
	}
	return limit, !ip, nil
}

func validOrigin(raw string, development bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Path == "" && api.ValidateURL(raw, development) == nil
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	development := os.Getenv("ENVIRONMENT") == "development"
	password := os.Getenv("OPERATOR_PASSWORD")
	if len(password) < 16 || len(password) > 72 {
		return errors.New("OPERATOR_PASSWORD must contain 16 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("password_setup_failed")
	}
	password = ""
	_ = os.Unsetenv("OPERATOR_PASSWORD")
	token := os.Getenv("ENGINE_TOKEN")
	if len(token) < 32 {
		return errors.New("ENGINE_TOKEN must contain at least 32 characters")
	}
	org := env("ORGANIZATION_ID", "community")
	var repo store.Repository
	var limiter ratelimit.Limiter
	if development && os.Getenv("DATABASE_URL") == "" {
		repo = store.NewMemory()
		limiter = ratelimit.NewMemory()
		log.Print("development: ephemeral metadata and quotas")
	} else {
		setup, done := context.WithTimeout(ctx, 30*time.Second)
		defer done()
		pool, err := pgxpool.New(setup, os.Getenv("DATABASE_URL"))
		if err != nil {
			return errors.New("database_configuration_invalid")
		}
		defer pool.Close()
		if migrations.Apply(setup, pool, org) != nil {
			return errors.New("database_setup_failed")
		}
		repo = &store.Postgres{Pool: pool}
		options, err := redis.ParseURL(os.Getenv("REDIS_URL"))
		if err != nil {
			return errors.New("redis_configuration_invalid")
		}
		options.ContextTimeoutEnabled = true
		client := redis.NewClient(options)
		defer client.Close()
		limiter = &ratelimit.Redis{Client: client}
		if limiter.Ready(setup) != nil {
			return errors.New("redis_unavailable")
		}
	}
	engineClient, err := engine.New(env("RUST_ENGINE_ADDR", "127.0.0.1:50051"), token)
	if err != nil {
		return errors.New("engine_connection_failed")
	}
	defer engineClient.Close()
	config := api.Config{Organization: org, PasswordHash: hash, Origin: env("PUBLIC_ORIGIN", "http://localhost:8080"), SecureCookies: !development, Assets: env("CONSOLE_ASSETS", "../../apps/web-console/dist"), OpenAIURL: env("OPENAI_BASE_URL", "https://api.openai.com/v1"), OpenAIKey: os.Getenv("OPENAI_API_KEY"), AnthropicURL: env("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1"), AnthropicKey: os.Getenv("ANTHROPIC_API_KEY"), GeminiURL: env("GEMINI_BASE_URL", api.DefaultGeminiURL), GeminiKey: os.Getenv("GEMINI_API_KEY")}
	config.MetricsToken = os.Getenv("METRICS_TOKEN")
	mode := env("DEPLOYMENT_MODE", "self-hosted")
	if mode != "self-hosted" && mode != "hosted" {
		return errors.New("invalid_deployment_mode")
	}
	config.Hosted = mode == "hosted"
	if err := validateHostedProviders(config); err != nil {
		return err
	}
	config.PublicEnabled, err = strconv.ParseBool(env("PUBLIC_ENABLED", "false"))
	if err != nil {
		return errors.New("invalid_public_enabled")
	}
	config.PublicHMACKey = os.Getenv("PUBLIC_HMAC_KEY")
	config.PublicDailyPerIP, config.DisableIPRateLimits, err = publicLimitSettings(development, os.Getenv)
	if err != nil {
		return err
	}
	queueSettings, err := workqueue.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	var queueStore workqueue.Store = workqueue.NewMemory()
	if shared, ok := limiter.(*ratelimit.Redis); ok {
		queueStore = workqueue.NewRedis(shared.Client, org)
	} else if os.Getenv("REDIS_URL") != "" {
		options, e := redis.ParseURL(os.Getenv("REDIS_URL"))
		if e != nil {
			return errors.New("redis_configuration_invalid")
		}
		options.ContextTimeoutEnabled = true
		queueClient := redis.NewClient(options)
		defer queueClient.Close()
		queueStore = workqueue.NewRedis(queueClient, org)
	}
	config.Queue = workqueue.New(queueStore, queueSettings)
	defer config.Queue.Close()
	queueReady, queueDone := context.WithTimeout(ctx, 3*time.Second)
	err = config.Queue.Ready(queueReady)
	queueDone()
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || prefix.Bits() == 0 {
			return errors.New("invalid_trusted_proxy_cidr")
		}
		config.TrustedProxies = append(config.TrustedProxies, prefix.Masked())
	}
	if config.PublicEnabled && len(config.PublicHMACKey) < 32 {
		return errors.New("PUBLIC_HMAC_KEY requires at least 32 characters")
	}
	for name, target := range map[string]*int{"PUBLIC_REQUESTS_PER_MINUTE": &config.PublicRPM, "PUBLIC_CONCURRENT_PER_IP": &config.PublicPerIP, "PUBLIC_CONCURRENT_GLOBAL": &config.PublicGlobal} {
		fallback := "10"
		if name == "PUBLIC_CONCURRENT_PER_IP" {
			fallback = "2"
		}
		if name == "PUBLIC_CONCURRENT_GLOBAL" {
			fallback = "20"
		}
		*target, err = strconv.Atoi(env(name, fallback))
		if err != nil || *target < 1 || *target > 100000 {
			return errors.New("invalid_public_limit")
		}
	}
	for _, raw := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if !validOrigin(origin, development) {
			return errors.New("invalid_allowed_origin")
		}
		config.AllowedOrigins = append(config.AllowedOrigins, origin)
	}
	budget, err := strconv.Atoi(env("DAILY_TOKEN_BUDGET", "1000000"))
	if err != nil || budget < 1 {
		return errors.New("invalid_daily_token_budget")
	}
	config.DailyTokenBudget = budget
	retention, err := strconv.Atoi(env("AUDIT_RETENTION_DAYS", "30"))
	if err != nil || retention < 1 || retention > 3650 {
		return errors.New("invalid_retention_days")
	}
	if database, ok := repo.(*store.Postgres); ok {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					cleanup, done := context.WithTimeout(ctx, 30*time.Second)
					_, err := database.Prune(cleanup, time.Now().UTC().AddDate(0, 0, -retention))
					done()
					if err != nil {
						log.Print("audit_retention_failed")
					}
				}
			}
		}()
	}
	if api.ValidateURL(config.OpenAIURL, development) != nil || api.ValidateURL(config.AnthropicURL, development) != nil || api.ValidateURL(config.GeminiURL, development) != nil {
		return errors.New("provider_configuration_invalid")
	}
	if !validOrigin(config.Origin, development) {
		return errors.New("invalid_public_origin")
	}
	server := &http.Server{Addr: env("GATEWAY_LISTEN", "127.0.0.1:8080"), Handler: api.New(config, repo, limiter, engineClient).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: queueSettings.Wait + queueSettings.Processing + 20*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		config.Queue.Close()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	log.Printf("Prompt Shield gateway listening on %s", server.Addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		client := &http.Client{Timeout: 4 * time.Second}
		response, err := client.Get("http://127.0.0.1:8080/readyz")
		if err != nil {
			os.Exit(1)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
