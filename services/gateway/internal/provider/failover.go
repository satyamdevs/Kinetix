package provider

import (
	"context"
	"errors"
	"log/slog"
)

type failoverProvider struct {
	name      string
	providers []Provider
}

func (f *failoverProvider) Name() string { return f.name }

func (f *failoverProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	var errs []error
	for _, p := range f.providers {
		resp, err := p.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		errs = append(errs, err)
		slog.Warn("provider failed, trying fallback",
			"provider", p.Name(),
			"model", req.Model,
			"err", err,
			"fallbacks_remaining", len(f.providers)-len(errs))
	}
	return nil, errors.Join(errs...)
}

func (f *failoverProvider) ProviderNames() []string {
	names := make([]string, len(f.providers))
	for i, p := range f.providers {
		names[i] = p.Name()
	}
	return names
}
