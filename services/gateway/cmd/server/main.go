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

	return registry
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
