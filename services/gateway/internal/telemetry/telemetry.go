package telemetry

import "time"

// Price defines per-unit token pricing for a model. Zero sub-rates mean the
// model is untracked and cost stays 0.
type Price struct {
	Input  float64 // USD per 1M input tokens
	Output float64 // USD per 1M output tokens
}

// Model costs are looked up by exact model name, else by a provider/family
// fallback supplied by the caller.
type Pricer struct {
	byModel map[string]Price
	ndigits float64
}

func NewPricer(models map[string]Price) *Pricer {
	return &Pricer{byModel: models}
}

// Model prices in USD per 1M tokens (approx; seeded defaults).
func DefaultPricer() *Pricer {
	return NewPricer(map[string]Price{
		"gpt-4o":                     {Input: 2.50, Output: 10.00},
		"gpt-4o-mini":                {Input: 0.15, Output: 0.60},
		"gpt-4":                      {Input: 30.00, Output: 60.00},
		"gpt-4-turbo":                {Input: 10.00, Output: 30.00},
		"gpt-3.5-turbo":              {Input: 0.50, Output: 1.50},
		"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00},
		"claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.00},
		"llama3.2":                   {Input: 0.10, Output: 0.40},
		"llama3.1":                   {Input: 0.10, Output: 0.40},
		"mistral":                    {Input: 0.10, Output: 0.40},
		"qwen2.5":                    {Input: 0.10, Output: 0.40},
	})
}

// Cost computes the USD cost of a request given usage.
func (p *Pricer) Cost(model string, prompt, completion int) float64 {
	pr, ok := p.byModel[model]
	if !ok {
		return 0
	}
	return float64(prompt)/1_000_000*pr.Input + float64(completion)/1_000_000*pr.Output
}

// Usage is the token accounting for a single completion.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Record is one observed completion persisted to the telemetry store.
type Record struct {
	RequestID  string
	APIKeyID   string
	Model      string
	Provider   string
	Streamed   bool
	Status     int
	TraceID    string
	Usage      Usage
	CostUSD    float64
	Latency    time.Duration
	Error      string
	FinishedAt time.Time
}
