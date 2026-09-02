package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
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

func handleChatCompletions(registry *provider.Registry, gov *Governance) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body: "+err.Error(), "invalid_request_error", "invalid_body")
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

		internalReq := toCompletionRequest(model, req.Messages)
		if req.Temperature > 0 {
			internalReq.Temperature = req.Temperature
		}
		if req.MaxTokens > 0 {
			internalReq.MaxTokens = req.MaxTokens
		}
		if req.Stream {
			streamChatCompletion(w, r, p, internalReq, gov)
			return
		}

		resp, err := p.Complete(r.Context(), internalReq)
		if err != nil {
			writeError(w, http.StatusBadGateway,
				"Upstream provider error: "+err.Error(),
				"api_error", "upstream_error")
			return
		}

		if d, checked := checkOutput(guardrailsOf(gov), responseText(resp)); checked && !d.OK {
			writeError(w, http.StatusBadRequest,
				"Response blocked by guardrail: "+d.Reason,
				"content_policy_violation", "content_policy")
			return
		}
		if err := spendQuota(r, limitOf(gov), int64(resp.Usage.TotalTokens)); err != nil {
			slog.Warn("quota accounting failed", "err", err)
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

func toCompletionRequest(model string, msgs []chatMessage) *provider.CompletionRequest {
	internalReq := &provider.CompletionRequest{
		Model:    model,
		Messages: make([]provider.Message, len(msgs)),
	}
	for i, m := range msgs {
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
	return internalReq
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

func responseText(resp *provider.CompletionResponse) string {
	var b strings.Builder
	for _, c := range resp.Choices {
		b.WriteString(c.Message.Content)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason any         `json:"finish_reason"`
}

type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
}

func writeSSE(w http.ResponseWriter, f http.Flusher, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	f.Flush()
	return nil
}

func streamChatCompletion(w http.ResponseWriter, r *http.Request, p provider.Provider, req *provider.CompletionRequest, gov *Governance) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError,
			"Streaming unsupported by this server.",
			"api_error", "streaming_unsupported")
		return
	}

	s, ok := p.(provider.Streamer)
	if !ok {
		writeError(w, http.StatusNotImplemented,
			"Configured provider does not support streaming.",
			"api_error", "streaming_unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	created := time.Now().Unix()

	roleChunk := streamChunk{
		Object:  "chat.completion.chunk",
		Created: created,
		Choices: []streamChoice{{
			Index: 0,
			Delta: streamDelta{Role: "assistant"},
		}},
	}
	if err := writeSSE(w, flusher, roleChunk); err != nil {
		return
	}

	var id, model string
	var usedTokens int64
	err := s.Stream(r.Context(), req, func(chunk provider.StreamChunk) error {
		if id == "" {
			id = chunk.ID
		}
		if model == "" {
			model = chunk.Model
		}
		payload := streamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []streamChoice{{
				Index: chunk.Index,
				Delta: streamDelta{Content: chunk.Delta},
			}},
		}
		if chunk.Finish != "" {
			payload.Choices[0].FinishReason = chunk.Finish
		}
		if err := writeSSE(w, flusher, payload); err != nil {
			return err
		}
		if chunk.Usage != nil {
			usedTokens = int64(chunk.Usage.TotalTokens)
			return writeSSE(w, flusher, map[string]any{
				"id":      id,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []streamChoice{},
				"usage":   chunk.Usage,
			})
		}
		return nil
	})
	if err != nil {
		_ = writeSSE(w, flusher, map[string]any{
			"error": map[string]any{
				"message": "Upstream provider error: " + err.Error(),
				"type":    "api_error",
			},
		})
	}
	if err := spendQuota(r, limitOf(gov), usedTokens); err != nil {
		slog.Warn("quota accounting failed", "err", err)
	}
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
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

func handleListModels(registry *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now().Unix()
		data := make([]modelEntry, 0, len(registry.Models()))
		for _, m := range registry.Models() {
			data = append(data, modelEntry{ID: m, Object: "model", Created: now, OwnedBy: "orca"})
		}
		writeJSON(w, http.StatusOK, modelList{Object: "list", Data: data})
	}
}
