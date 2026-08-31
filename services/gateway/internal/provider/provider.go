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
	providers map[string][]Provider
	order     []string
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string][]Provider),
	}
}

func (r *Registry) Register(model string, p Provider) {
	r.providers[model] = append(r.providers[model], p)
	r.recordOrder(model)
}

func (r *Registry) RegisterWithFallbacks(model string, ps ...Provider) {
	r.providers[model] = append(r.providers[model], ps...)
	r.recordOrder(model)
}

func (r *Registry) recordOrder(model string) {
	for _, existing := range r.order {
		if existing == model {
			return
		}
	}
	r.order = append(r.order, model)
}

func (r *Registry) Lookup(model string) (Provider, error) {
	providers, ok := r.providers[model]
	if !ok || len(providers) == 0 {
		return nil, fmt.Errorf("no provider configured for model %q", model)
	}
	return &failoverProvider{name: model, providers: providers}, nil
}

func (r *Registry) Models() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

func (r *Registry) ProviderNames(model string) []string {
	providers, ok := r.providers[model]
	if !ok {
		return nil
	}
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return names
}