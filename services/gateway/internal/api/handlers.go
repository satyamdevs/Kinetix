package api

import (
	"encoding/json"
	"net/http"
	"time"
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

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
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
		model = "orca-fast"
	}

	promptTokens := len(req.Messages) * 8
	resp := chatCompletion{
		ID:                "chatcmpl-mock-" + randomID(),
		Object:            "chat.completion",
		Created:           time.Now().Unix(),
		Model:             model,
		SystemFingerprint: "orca_mock_v1",
		Choices: []choice{{
			Index: 0,
			Message: choiceMessage{
				Role:    "assistant",
				Content: "This is a mock completion from Orca gateway.",
			},
			FinishReason: "stop",
		}},
		Usage: usage{
			PromptTokens:     promptTokens,
			CompletionTokens: 9,
			TotalTokens:      promptTokens + 9,
		},
	}
	writeJSON(w, http.StatusOK, resp)
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
