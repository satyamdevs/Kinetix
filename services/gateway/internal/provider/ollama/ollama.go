package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"orca/gateway/internal/provider"
)

const defaultBaseURL = "http://localhost:11434/v1"

type Config struct {
	BaseURL string
}

type adapter struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) provider.Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = baseURL

	return &adapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (a *adapter) Name() string { return "ollama" }

type ollamaRequest struct {
	Model       string             `json:"model"`
	Messages    []provider.Message `json:"messages"`
	Stream      bool               `json:"stream"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_completion_tokens,omitempty"`
}

type ollamaResponse struct {
	ID                string         `json:"id"`
	Model             string         `json:"model"`
	Choices           []ollamaChoice `json:"choices"`
	Usage             ollamaUsage    `json:"usage"`
	SystemFingerprint string         `json:"system_fingerprint"`
}

type ollamaChoice struct {
	Index        int              `json:"index"`
	Message      provider.Message `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type ollamaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ollamaErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (a *adapter) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	ollamaReq := ollamaRequest{
		Model:    req.Model,
		Stream:   false,
		Messages: req.Messages,
	}
	if req.Temperature > 0 {
		ollamaReq.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		ollamaReq.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama request: %w", err)
	}

	url := a.cfg.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	slog.Debug("ollama request", "model", req.Model, "url", url)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ollama response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var oErr ollamaErrorResponse
		if json.Unmarshal(respBody, &oErr) == nil && oErr.Error.Message != "" {
			return nil, fmt.Errorf("ollama api error (%s): %s", oErr.Error.Type, oErr.Error.Message)
		}
		return nil, fmt.Errorf("ollama api returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("unmarshal ollama response: %w", err)
	}

	choices := make([]provider.Choice, len(ollamaResp.Choices))
	for i, c := range ollamaResp.Choices {
		choices[i] = provider.Choice{
			Index:        c.Index,
			Message:      c.Message,
			FinishReason: c.FinishReason,
		}
	}

	resp := &provider.CompletionResponse{
		ID:                ollamaResp.ID,
		Model:             ollamaResp.Model,
		SystemFingerprint: ollamaResp.SystemFingerprint,
		Choices:           choices,
		Usage: provider.Usage{
			PromptTokens:     ollamaResp.Usage.PromptTokens,
			CompletionTokens: ollamaResp.Usage.CompletionTokens,
			TotalTokens:      ollamaResp.Usage.TotalTokens,
		},
	}

	slog.Debug("ollama response", "id", resp.ID, "model", resp.Model, "tokens", resp.Usage.TotalTokens)
	return resp, nil
}
