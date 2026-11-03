package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/ingress"
	"github.com/redis/go-redis/v9"
)

func dailyEnabled(environment, setting string) (bool, error) {
	switch setting {
	case "", "true":
		return environment != "development", nil
	case "false":
		return false, nil
	default:
		return false, errors.New("invalid_ingress_configuration")
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://127.0.0.1:8080/ingress/healthz")
		if err != nil {
			return errors.New("ingress_unhealthy")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return errors.New("ingress_unhealthy")
		}
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	upstream, err := url.Parse(os.Getenv("INGRESS_UPSTREAM"))
	if err != nil {
		return errors.New("invalid_ingress_configuration")
	}
	options, err := redis.ParseURL(os.Getenv("INGRESS_REDIS_URL"))
	if err != nil {
		return errors.New("invalid_ingress_configuration")
	}
	options.ContextTimeoutEnabled = true
	options.DialTimeout, options.ReadTimeout, options.WriteTimeout = 2*time.Second, 2*time.Second, 2*time.Second
	options.MaxRetries = -1
	client := redis.NewClient(options)
	defer client.Close()
	cfg := ingress.Config{Upstream: upstream, Origin: os.Getenv("PUBLIC_ORIGIN"), Product: os.Getenv("INGRESS_PRODUCT"), HMACKey: os.Getenv("PUBLIC_HMAC_KEY"), MetricsToken: os.Getenv("METRICS_TOKEN")}
	cfg.DailyEnabled, err = dailyEnabled(os.Getenv("ENVIRONMENT"), os.Getenv("PUBLIC_DAILY_IP_LIMIT_ENABLED"))
	if err != nil {
		return err
	}
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(value) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return errors.New("invalid_ingress_configuration")
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix)
	}
	handler, err := ingress.New(cfg, ingress.RedisCounter{Client: client})
	if err != nil {
		return errors.New("invalid_ingress_configuration")
	}
	if cfg.DailyEnabled {
		ready, done := context.WithTimeout(ctx, 3*time.Second)
		err := client.Ping(ready).Err()
		done()
		if err != nil {
			return errors.New("ingress_store_unavailable")
		}
	}
	server := &http.Server{Addr: ":8080", ErrorLog: log.New(io.Discard, "", 0)}
	// A context deadline covers response streaming and cancels upstream dispatch.
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestContext, done := context.WithTimeout(r.Context(), 400*time.Second)
		defer done()
		handler.ServeHTTP(w, r.WithContext(requestContext))
	})
	server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout = 5*time.Second, 15*time.Second, 405*time.Second, 60*time.Second
	server.MaxHeaderBytes = 32 * 1024
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	if run() != nil {
		log.Fatal("ingress_startup_or_runtime_failed")
	}
}
