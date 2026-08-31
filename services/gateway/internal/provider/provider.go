package provider

import (
	"context"
	"fmt"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

func (r *Registry) Register(model string, p Provider) {
	r.providers[model] = p
}

func (r *Registry) Lookup(model string) (Provider, error) {
	p, ok := r.providers[model]
	if !ok {
		return nil, fmt.Errorf("no provider configured for model %q", model)
	}
	return p, nil
}
