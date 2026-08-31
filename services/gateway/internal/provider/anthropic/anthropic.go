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
	defaultBaseURL    = "https://api.anthropic.com/v1"
	defaultAPIVersion = "2023-06-01"
	defaultMaxTokens  = 1024
	systemRole        = "system"
	assistantRole     = "assistant"
	userRole          = "user"
)

type Config struct {
	APIKey     string
	BaseURL    string
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
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

type anthropicResponse struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Model   string           `json:"model"`
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
	Usage   anthropicUsage   `json:"usage"`
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

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
}

func (a *adapter) buildRequest(req *provider.CompletionRequest, stream bool) (*bytes.Buffer, error) {
	anthropicReq := anthropicRequest{
		Model:     req.Model,
		MaxTokens: defaultMaxTokens,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
		Stream:    stream,
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
	return bytes.NewBuffer(body), nil
}

func (a *adapter) post(ctx context.Context, body *bytes.Buffer) (*http.Response, error) {
	url := a.cfg.BaseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", a.cfg.APIVersion)

	slog.Debug("anthropic request", "url", url)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		var aErr anthropicErrorResponse
		if json.Unmarshal(respBody, &aErr) == nil && aErr.Error.Message != "" {
			return nil, fmt.Errorf("anthropic api error (%s): %s", aErr.Error.Type, aErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic api returned status %d: %s", httpResp.StatusCode, string(respBody))
	}
	return httpResp, nil
}

func (a *adapter) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	body, err := a.buildRequest(req, false)
	if err != nil {
		return nil, err
	}

	httpResp, err := a.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic response: %w", err)
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
		ID:    anthropicResp.ID,
		Model: anthropicResp.Model,
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

func (a *adapter) Stream(ctx context.Context, req *provider.CompletionRequest, emit func(provider.StreamChunk) error) error {
	body, err := a.buildRequest(req, true)
	if err != nil {
		return err
	}

	httpResp, err := a.post(ctx, body)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	var id, model string
	var inputTokens, outputTokens int

	return provider.ForEachSSE(httpResp.Body, func(data string) error {
		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("unmarshal anthropic stream event: %w", err)
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				id = ev.Message.ID
				model = ev.Message.Model
			}
		case "content_block_delta":
			if ev.Delta != nil && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				if err := emit(provider.StreamChunk{ID: id, Model: model, Index: 0, Delta: ev.Delta.Text}); err != nil {
					return err
				}
			}
		case "message_delta":
			if ev.Usage != nil {
				outputTokens = ev.Usage.OutputTokens
			}
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				return emit(provider.StreamChunk{
					ID:     id,
					Model:  model,
					Index:  0,
					Finish: ev.Delta.StopReason,
					Usage: &provider.Usage{
						PromptTokens:     inputTokens,
						CompletionTokens: outputTokens,
						TotalTokens:      inputTokens + outputTokens,
					},
				})
			}
		}
		return nil
	})
}
