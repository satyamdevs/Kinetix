package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"orca/gateway/internal/api"
	"orca/gateway/internal/guardrails"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
	"orca/gateway/internal/provider/anthropic"
	"orca/gateway/internal/provider/ollama"
	"orca/gateway/internal/provider/openai"
	"orca/gateway/internal/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	gov := buildGovernance(ctx)
	registry := buildProviderRegistry()
	srv := &http.Server{
		Addr:              ":" + envOr("ORCA_PORT", "8080"),
		Handler:           api.NewRouter(gov, registry),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("graceful shutdown failed", "err", err)
		}
		close(shutdownDone)
	}()

	slog.Info("gateway listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
	<-shutdownDone
	if gov.Telemetry != nil {
		if err := gov.Telemetry.Close(); err != nil {
			slog.Warn("closing telemetry sink", "err", err)
		}
	}
	slog.Info("gateway stopped")
}

func buildGovernance(ctx context.Context) *api.Governance {
	gov := &api.Governance{
		Limiter: limits.NewMemory(),
		Policy: limits.Policy{
			RequestsPerMinute: envInt("ORCA_DEFAULT_RPM", 1000),
			TokensPerMonth:    envInt("ORCA_DEFAULT_TPM", 100_000_000),
			Window:            limits.DefaultWindow,
		},
	}

	gov.Keys, gov.Keeper = buildKeyStore(ctx)
	gov.Limiter = buildLimiter(ctx)
	gov.Guardrails = buildGuardrails()
	gov.Telemetry, gov.Querier = buildTelemetry(ctx)
	gov.Pricer = telemetry.DefaultPricer()

	slog.Info("governance configured",
		"key_store", storeKind(gov.Keys),
		"limiter", limiterKind(gov.Limiter),
		"guardrails", guardrailCount(gov.Guardrails),
		"telemetry", telemetryKind(gov.Telemetry),
		"default_rpm", gov.Policy.RequestsPerMinute,
	)
	return gov
}

func buildTelemetry(ctx context.Context) (telemetry.Sink, telemetry.Querier) {
	if dsn := os.Getenv("CLICKHOUSE_DSN"); dsn != "" {
		sink, err := telemetry.Open(ctx, dsn)
		if err != nil {
			slog.Warn("clickhouse unavailable, telemetry buffered in memory", "err", err)
			buf := telemetry.NewBuffer(1024)
			return buf, &telemetry.InMemoryQuerier{Buffer: buf}
		}
		return sink, sink
	}
	buf := telemetry.NewBuffer(1024)
	return buf, &telemetry.InMemoryQuerier{Buffer: buf}
}

func telemetryKind(s telemetry.Sink) string {
	switch s.(type) {
	case *telemetry.ClickHouse:
		return "clickhouse"
	case *telemetry.Buffer:
		return "memory"
	default:
		return "none"
	}
}

func buildKeyStore(ctx context.Context) (keys.Store, keys.Keeper) {
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		store, err := keys.NewPostgres(ctx, dsn)
		if err != nil {
			slog.Error("postgres unavailable, falling back to in-memory keys", "err", err)
		} else {
			return store, store
		}
	}

	mem := keys.NewMemory()
	limit := keys.Limit{
		RequestsPerMinute: envInt("ORCA_DEFAULT_RPM", 1000),
		TokensPerMonth:    envInt("ORCA_DEFAULT_TPM", 100_000_000),
	}
	for _, raw := range apiKeysFromEnv() {
		mem.Upsert(&keys.Key{
			ID:        "dev-" + keys.Hash(raw)[:8],
			Name:      "dev",
			Owner:     "local",
			Hash:      keys.Hash(raw),
			Prefix:    raw[:8],
			Limit:     limit,
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
		})
	}
	if raw := os.Getenv("ORCA_ADMIN_KEY"); raw != "" {
		mem.Upsert(&keys.Key{
			ID:        "admin-" + keys.Hash(raw)[:8],
			Name:      "admin",
			Owner:     "local",
			Hash:      keys.Hash(raw),
			Prefix:    raw[:8],
			Scopes:    []string{"admin"},
			Limit:     limit,
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
		})
	}
	return mem, mem
}

func buildLimiter(ctx context.Context) limits.Limiter {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		if err := rdb.Ping(ctx).Err(); err != nil {
			slog.Error("redis unavailable, falling back to in-memory limiter", "err", err)
			return limits.NewMemory()
		}
		return limits.NewRedis(rdb)
	}
	return limits.NewMemory()
}

func buildGuardrails() *guardrails.Filter {
	entries := splitCSV(os.Getenv("ORCA_GUARDRAILS"))
	if len(entries) == 0 {
		return nil
	}
	rules, err := guardrails.ParseRules(entries)
	if err != nil {
		slog.Error("invalid ORCA_GUARDRAILS, disabling guardrails", "err", err)
		return nil
	}
	return guardrails.New(rules...)
}

func buildProviderRegistry() *provider.Registry {
	registry := provider.NewRegistry()

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		adapter := openai.New(openai.Config{
			APIKey:  apiKey,
			BaseURL: os.Getenv("OPENAI_BASE_URL"),
		})
		for _, m := range []string{"gpt-4o", "gpt-4o-mini", "gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"} {
			registry.Register(m, adapter)
		}
		slog.Info("registered openai provider", "models", "gpt-4o, gpt-4o-mini, gpt-4, gpt-4-turbo, gpt-3.5-turbo")
	} else {
		slog.Warn("OPENAI_API_KEY not set; OpenAI provider disabled")
	}

	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		adapter := anthropic.New(anthropic.Config{
			APIKey:     apiKey,
			BaseURL:    os.Getenv("ANTHROPIC_BASE_URL"),
			APIVersion: os.Getenv("ANTHROPIC_API_VERSION"),
		})
		for _, m := range []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022"} {
			registry.Register(m, adapter)
		}
		slog.Info("registered anthropic provider", "models", "claude-3-5-sonnet-20241022, claude-3-5-haiku-20241022")
	} else {
		slog.Warn("ANTHROPIC_API_KEY not set; Anthropic provider disabled")
	}

	ollamaAdapter := ollama.New(ollama.Config{
		BaseURL: envOr("OLLAMA_BASE_URL", "http://localhost:11434/v1"),
	})
	for _, m := range []string{"llama3.2", "llama3.1", "mistral", "qwen2.5"} {
		registry.Register(m, ollamaAdapter)
	}
	slog.Info("registered ollama provider", "base_url", envOr("OLLAMA_BASE_URL", "http://localhost:11434/v1"))

	registerFallbacks(registry,
		"orca-fast", "gpt-4o-mini", "claude-3-5-haiku-20241022", "llama3.2")
	registerFallbacks(registry,
		"orca-smart", "gpt-4o", "claude-3-5-sonnet-20241022", "llama3.1")

	return registry
}

func registerFallbacks(registry *provider.Registry, alias string, models ...string) {
	var available []provider.Provider
	for _, m := range models {
		if p, err := registry.Lookup(m); err == nil {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		slog.Warn("no providers available for alias", "alias", alias)
		return
	}
	registry.RegisterWithFallbacks(alias, available...)
	slog.Info("registered fallback alias", "alias", alias, "providers", registry.ProviderNames(alias))
}

func storeKind(s keys.Store) string {
	switch s.(type) {
	case *keys.Postgres:
		return "postgres"
	case *keys.Memory:
		return "memory"
	default:
		return "none"
	}
}

func limiterKind(l limits.Limiter) string {
	switch l.(type) {
	case *limits.Redis:
		return "redis"
	case *limits.Memory:
		return "memory"
	default:
		return "none"
	}
}

func guardrailCount(f *guardrails.Filter) int {
	if f == nil {
		return 0
	}
	return f.Len()
}

func apiKeysFromEnv() []string {
	raw := envOr("ORCA_API_KEYS", "sk-orca-dev-key")
	var keys []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func splitCSV(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		slog.Warn("invalid int", "key", key, "value", v, "err", err)
		return fallback
	}
	return n
}
