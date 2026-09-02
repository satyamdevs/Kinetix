package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"orca/gateway/internal/guardrails"
	"orca/gateway/internal/keys"
	"orca/gateway/internal/limits"
	"orca/gateway/internal/provider"
	"orca/gateway/internal/telemetry"
)

// Governance bundles the control-plane pieces applied to every /v1 request.
// Nil Limiter and Guardrails disable those checks; a nil Keys store rejects
// all authenticated traffic. Telemetry is best-effort and never fails a request.
type Governance struct {
	Keys       keys.Store
	Limiter    limits.Limiter
	Guardrails *guardrails.Filter
	Policy     limits.Policy
	Telemetry  telemetry.Sink
	Pricer     *telemetry.Pricer
}

func NewRouter(gov *Governance, registry *provider.Registry) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz)

	v1 := chi.NewRouter()
	v1.Use(authMiddleware(gov))
	v1.Use(RateLimit(limitOf(gov), policyOf(gov)))
	v1.Use(Quota(limitOf(gov)))
	v1.Use(GuardrailInput(guardrailsOf(gov)))
	v1.Post("/chat/completions", handleChatCompletions(registry, gov))
	v1.Get("/models", handleListModels(registry))
	r.Mount("/v1", v1)

	return r
}

func authMiddleware(gov *Governance) func(http.Handler) http.Handler {
	if gov == nil || gov.Keys == nil {
		return rejectAll
	}
	return Authenticate(gov.Keys)
}

func limitOf(gov *Governance) limits.Limiter {
	if gov == nil {
		return nil
	}
	return gov.Limiter
}

func policyOf(gov *Governance) limits.Policy {
	if gov == nil {
		return limits.Policy{}
	}
	return gov.Policy
}

func guardrailsOf(gov *Governance) *guardrails.Filter {
	if gov == nil {
		return nil
	}
	return gov.Guardrails
}

func rejectAll(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusServiceUnavailable,
			"API key store is not configured.",
			"api_error", "governance_unconfigured")
	})
}
