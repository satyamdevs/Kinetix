package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"orca/gateway/internal/provider"
)

func NewRouter(apiKeys []string, registry *provider.Registry) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz)

	v1 := chi.NewRouter()
	v1.Use(RequireBearerKey(apiKeys))
	v1.Post("/chat/completions", handleChatCompletions(registry))
	v1.Get("/models", handleListModels(registry))
	r.Mount("/v1", v1)

	return r
}
