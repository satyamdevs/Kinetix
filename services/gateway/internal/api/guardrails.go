package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"orca/gateway/internal/guardrails"
	"orca/gateway/internal/observability"
)

// GuardrailInput inspects the inbound chat request for policy violations
// before it reaches a provider. It round-trips the body so downstream
// handlers still decode it normally.
func GuardrailInput(f *guardrails.Filter, m *observability.Metrics) func(http.Handler) http.Handler {
	if f == nil || f.Len() == 0 {
		return passthrough()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest,
					"Could not read request body.",
					"invalid_request_error", "invalid_body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			var req chatRequest
			if err := json.Unmarshal(body, &req); err == nil && len(req.Messages) > 0 {
				var b strings.Builder
				for _, m := range req.Messages {
					if m.Role == "assistant" {
						continue
					}
					b.WriteString(m.Role + ": ")
					b.WriteString(joinRawContent(m.Content))
					b.WriteString("\n")
				}
				if d := f.Check(b.String(), guardrails.RoleInput); !d.OK {
					m.Rejected(observability.RejectedGuardrail)
					writeError(w, http.StatusBadRequest,
						"Request blocked by guardrail: "+d.Reason,
						"content_policy_violation", "content_policy")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkOutput evaluates model output against output-scoped rules.
func checkOutput(f *guardrails.Filter, text string) (guardrails.Decision, bool) {
	if f == nil || f.Len() == 0 {
		return guardrails.Decision{OK: true}, false
	}
	return f.Check(text, guardrails.RoleOutput), true
}

func joinRawContent(raw json.RawMessage) string {
	var txt string
	if len(raw) > 0 && json.Unmarshal(raw, &txt) == nil {
		return txt
	}
	if len(raw) > 0 {
		var parts []string
		if json.Unmarshal(raw, &parts) == nil {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}
