package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"orca/gateway/internal/api"
	"orca/gateway/internal/provider"
	"orca/gateway/internal/provider/anthropic"
	"orca/gateway/internal/provider/ollama"
	"orca/gateway/internal/provider/openai"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	keys := apiKeysFromEnv()
	registry := buildProviderRegistry()
	srv := &http.Server{
		Addr:              ":" + envOr("ORCA_PORT", "8080"),
		Handler:           api.NewRouter(keys, registry),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
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
	slog.Info("gateway stopped")
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
