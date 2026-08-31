package openai

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

const defaultBaseURL = "https://api.openai.com/v1"

type Config struct {
	APIKey  string
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

func (a *adapter) Name() string { return "openai" }

type openaiRequest struct {
	Model       string            `json:"model"`
	Messages    []openaiMessage   `json:"messages"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	ID                string         `json:"id"`
	Model             string         `json:"model"`
	Choices           []openaiChoice `json:"choices"`
	Usage             openaiUsage    `json:"usage"`
	SystemFingerprint string         `json:"system_fingerprint"`
}

type openaiChoice struct {
	Index        int              `json:"index"`
	Message      openaiMessage    `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (a *adapter) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	oaiReq := openaiRequest{
		Model:    req.Model,
		Stream:   false,
		Messages: make([]openaiMessage, len(req.Messages)),
	}
	for i, m := range req.Messages {
		oaiReq.Messages[i] = openaiMessage{Role: m.Role, Content: m.Content}
	}
	if req.Temperature > 0 {
		oaiReq.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		oaiReq.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	url := a.cfg.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)

	slog.Debug("openai request", "model", req.Model, "url", url)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var oaiErr openaiErrorResponse
		if json.Unmarshal(respBody, &oaiErr) == nil && oaiErr.Error.Message != "" {
			return nil, fmt.Errorf("openai api error (%s): %s", oaiErr.Error.Type, oaiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai api returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal openai response: %w", err)
	}

	resp := &provider.CompletionResponse{
		ID:                oaiResp.ID,
		Model:             oaiResp.Model,
		SystemFingerprint: oaiResp.SystemFingerprint,
		Choices:           make([]provider.Choice, len(oaiResp.Choices)),
		Usage: provider.Usage{
			PromptTokens:     oaiResp.Usage.PromptTokens,
			CompletionTokens: oaiResp.Usage.CompletionTokens,
			TotalTokens:      oaiResp.Usage.TotalTokens,
		},
	}
	for i, c := range oaiResp.Choices {
		resp.Choices[i] = provider.Choice{
			Index:        c.Index,
			Message:      provider.Message{Role: c.Message.Role, Content: c.Message.Content},
			FinishReason: c.FinishReason,
		}
	}

	slog.Debug("openai response", "id", resp.ID, "model", resp.Model, "tokens", resp.Usage.TotalTokens)
	return resp, nil
}
