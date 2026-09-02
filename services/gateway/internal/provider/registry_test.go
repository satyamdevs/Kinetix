package provider

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type stubProvider struct {
	name       string
	completeFn func(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	streamFn   func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if s.completeFn != nil {
		return s.completeFn(ctx, req)
	}
	return &CompletionResponse{ID: s.name, Model: req.Model}, nil
}

func (s *stubProvider) Stream(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
	if s.streamFn != nil {
		return s.streamFn(ctx, req, emit)
	}
	return nil
}

type noStreamProvider struct {
	name string
	err  error
}

func (n *noStreamProvider) Name() string { return n.name }

func (n *noStreamProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	return nil, n.err
}

func fail(name string) *stubProvider {
	return &stubProvider{name: name, completeFn: func(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
		return nil, errors.New("boom " + name)
	}}
}

func TestRegistryRegisterAndModels(t *testing.T) {
	r := NewRegistry()
	r.Register("a", fail("a"))
	r.Register("b", fail("b"))
	r.RegisterWithFallbacks("alias", fail("a"), fail("b"))

	want := []string{"a", "b", "alias"}
	if got := r.Models(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Models() = %v, want %v", got, want)
	}
}

func TestRegistryDuplicateModelListedOnce(t *testing.T) {
	r := NewRegistry()
	r.Register("a", fail("a"))
	r.Register("a", fail("a2"))
	if got := r.Models(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Models() = %v, want single \"a\"", got)
	}
	if names := r.ProviderNames("a"); !reflect.DeepEqual(names, []string{"a", "a2"}) {
		t.Fatalf("ProviderNames(a) = %v", names)
	}
}

func TestRegistryLookupUnknown(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Lookup("nope"); err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestFailoverCompleteFallsThrough(t *testing.T) {
	r := NewRegistry()
	r.RegisterWithFallbacks("alias", fail("first"), &stubProvider{name: "second"})

	p, err := r.Lookup("alias")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(context.Background(), &CompletionRequest{Model: "alias"})
	if err != nil {
		t.Fatalf("expected fallback success: %v", err)
	}
	if resp.ID != "second" {
		t.Fatalf("resp from %q, want second", resp.ID)
	}
}

func TestFailoverCompleteAllFail(t *testing.T) {
	r := NewRegistry()
	r.RegisterWithFallbacks("alias", fail("one"), fail("two"))

	p, _ := r.Lookup("alias")
	_, err := p.Complete(context.Background(), &CompletionRequest{Model: "alias"})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "one") || !strings.Contains(err.Error(), "two") {
		t.Fatalf("joined error missing sources: %v", err)
	}
}

func TestFailoverStreamCollectsFromSecond(t *testing.T) {
	first := &stubProvider{name: "first", streamFn: func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
		return errors.New("stream boom")
	}}
	second := &stubProvider{name: "second", streamFn: func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
		_ = emit(StreamChunk{Delta: "hello "})
		_ = emit(StreamChunk{Delta: "world"})
		return nil
	}}

	f := &failoverProvider{name: "alias", providers: []Provider{first, second}}
	var got []string
	err := f.Stream(context.Background(), &CompletionRequest{Model: "alias"}, func(chunk StreamChunk) error {
		got = append(got, chunk.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}
	want := []string{"hello ", "world"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collected %v, want %v", got, want)
	}
}

func TestFailoverStreamSkipsNonStreamer(t *testing.T) {
	f := &failoverProvider{name: "alias", providers: []Provider{
		&noStreamProvider{name: "plain", err: errors.New("n/a")},
		&stubProvider{name: "streamy"},
	}}
	err := f.Stream(context.Background(), &CompletionRequest{Model: "alias"}, func(chunk StreamChunk) error { return nil })
	if err != nil {
		t.Fatalf("Stream() = %v, want nil after skipping non-streamer", err)
	}
}

func TestFailoverStreamNoFallbackAfterMidStream(t *testing.T) {
	first := &stubProvider{name: "first", streamFn: func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
		_ = emit(StreamChunk{Delta: "partial "})
		return errors.New("mid-stream boom")
	}}
	second := &stubProvider{name: "second", streamFn: func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
		return emit(StreamChunk{Delta: "should not appear"})
	}}

	f := &failoverProvider{name: "alias", providers: []Provider{first, second}}
	var got []string
	err := f.Stream(context.Background(), &CompletionRequest{Model: "alias"}, func(chunk StreamChunk) error {
		got = append(got, chunk.Delta)
		return nil
	})
	if err == nil {
		t.Fatal("expected mid-stream error to propagate, not fall back")
	}
	if !strings.Contains(err.Error(), "mid-stream") {
		t.Fatalf("err = %v, want mid-stream mention", err)
	}
	if len(got) != 1 || got[0] != "partial " {
		t.Fatalf("collected %v, want only first provider's partial delta", got)
	}
}

func TestFailoverStreamPropagatesEmitError(t *testing.T) {
	f := &failoverProvider{name: "alias", providers: []Provider{
		&stubProvider{name: "streamy", streamFn: func(ctx context.Context, req *CompletionRequest, emit func(StreamChunk) error) error {
			return emit(StreamChunk{Delta: "x"})
		}},
	}}
	stop := errors.New("client gone")
	err := f.Stream(context.Background(), &CompletionRequest{Model: "alias"}, func(chunk StreamChunk) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("Stream() = %v, want emit error", err)
	}
}
