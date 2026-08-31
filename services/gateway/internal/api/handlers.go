package api

import (
	"encoding/json"
	"net/http"
	"time"

	"orca/gateway/internal/provider"
)

type apiErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

func writeError(w http.ResponseWriter, status int, message, errType, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]apiErrorBody{"error": {
		Message: message,
		Type:    errType,
		Code:    code,
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReadyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type choiceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type choice struct {
	Index        int           `json:"index"`
	Message      choiceMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatCompletion struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	SystemFingerprint string   `json:"system_fingerprint"`
	Choices           []choice `json:"choices"`
	Usage             usage    `json:"usage"`
}

func handleChatCompletions(registry *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body: "+err.Error(), "invalid_request_error", "invalid_body")
			return
		}

		if req.Stream {
			writeError(w, http.StatusNotImplemented,
				"Streaming is not implemented yet; retry with \"stream\": false",
				"api_error", "streaming_not_implemented")
			return
		}

		model := req.Model
		if model == "" {
			writeError(w, http.StatusBadRequest,
				"The \"model\" field is required.",
				"invalid_request_error", "missing_model")
			return
		}

		p, err := registry.Lookup(model)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				err.Error(),
				"invalid_request_error", "unknown_model")
			return
		}

		internalReq := &provider.CompletionRequest{
			Model:    model,
			Messages: make([]provider.Message, len(req.Messages)),
		}
		for i, m := range req.Messages {
			txt := ""
			if len(m.Content) > 0 {
				_ = json.Unmarshal(m.Content, &txt)
				if txt == "" {
					var parts []string
					if json.Unmarshal(m.Content, &parts) == nil {
						txt = joinStringParts(parts)
					}
				}
			}
			internalReq.Messages[i] = provider.Message{Role: m.Role, Content: txt}
		}

		resp, err := p.Complete(r.Context(), internalReq)
		if err != nil {
			writeError(w, http.StatusBadGateway,
				"Upstream provider error: "+err.Error(),
				"api_error", "upstream_error")
			return
		}

		out := chatCompletion{
			ID:                resp.ID,
			Object:            "chat.completion",
			Created:           time.Now().Unix(),
			Model:             resp.Model,
			SystemFingerprint: resp.SystemFingerprint,
			Choices:           make([]choice, len(resp.Choices)),
			Usage: usage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
			},
		}
		for i, c := range resp.Choices {
			out.Choices[i] = choice{
				Index: c.Index,
				Message: choiceMessage{
					Role:    c.Message.Role,
					Content: c.Message.Content,
				},
				FinishReason: c.FinishReason,
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func joinStringParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type modelList struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

func handleListModels(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().Unix()
	writeJSON(w, http.StatusOK, modelList{
		Object: "list",
		Data: []modelEntry{
			{ID: "orca-fast", Object: "model", Created: now, OwnedBy: "orca"},
			{ID: "orca-smart", Object: "model", Created: now, OwnedBy: "orca"},
		},
	})
}