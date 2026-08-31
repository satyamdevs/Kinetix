package anthropic

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

const (
	defaultBaseURL       = "https://api.anthropic.com/v1"
	defaultAPIVersion    = "2023-06-01"
	defaultMaxTokens     = 1024
	systemRole           = "system"
	assistantRole        = "assistant"
	userRole             = "user"
)

type Config struct {
	APIKey    string
	BaseURL   string
	APIVersion string
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
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	cfg.BaseURL = baseURL

	return &adapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (a *adapter) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	System      string            `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

type anthropicResponse struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"`
	Model  string            `json:"model"`
	Role   string            `json:"role"`
	Content []anthropicBlock  `json:"content"`
	Usage  anthropicUsage    `json:"usage"`
}

type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *adapter) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	anthropicReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: defaultMaxTokens,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
	}

	var system strings.Builder
	for _, m := range req.Messages {
		switch m.Role {
		case systemRole:
			if system.Len() > 0 {
				system.WriteString("\n")
			}
			system.WriteString(m.Content)
		case assistantRole, userRole:
			anthropicReq.Messages = append(anthropicReq.Messages, anthropicMessage{Role: m.Role, Content: m.Content})
		}
	}
	if system.Len() > 0 {
		anthropicReq.System = system.String()
	}
	if req.MaxTokens > 0 {
		anthropicReq.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		anthropicReq.Temperature = req.Temperature
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	url := a.cfg.BaseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", a.cfg.APIVersion)

	slog.Debug("anthropic request", "model", req.Model, "url", url)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var aErr anthropicErrorResponse
		if json.Unmarshal(respBody, &aErr) == nil && aErr.Error.Message != "" {
			return nil, fmt.Errorf("anthropic api error (%s): %s", aErr.Error.Type, aErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic api returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("unmarshal anthropic response: %w", err)
	}

	var text strings.Builder
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(block.Text)
		}
	}

	resp := &provider.CompletionResponse{
		ID:     anthropicResp.ID,
		Model:  anthropicResp.Model,
		Choices: []provider.Choice{{
			Index:        0,
			Message:      provider.Message{Role: assistantRole, Content: text.String()},
			FinishReason: "stop",
		}},
		Usage: provider.Usage{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}

	slog.Debug("anthropic response", "id", resp.ID, "model", resp.Model, "tokens", resp.Usage.TotalTokens)
	return resp, nil
}