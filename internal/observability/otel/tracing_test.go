package otel

import (
	"context"
	"testing"
	"time"
)

func TestSamplerFromEnv(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	if name, ratio := samplerRatio(); name != "parentbased_traceidratio" || ratio != 1 {
		t.Fatalf("default sampler = %q/%v", name, ratio)
	}

	t.Setenv("OTEL_TRACES_SAMPLER", "Always_Off")
	if name := samplerName(); name != "always_off" {
		t.Fatalf("sampler name = %q", name)
	}

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	if name, ratio := samplerRatio(); name != "always_off" || ratio != 0.25 {
		t.Fatalf("sampler ratio = %q/%v", name, ratio)
	}

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "bad")
	if _, ratio := samplerRatio(); ratio != 1 {
		t.Fatalf("invalid ratio = %v, want fallback 1", ratio)
	}
}

func TestShutdownWithTimeout(t *testing.T) {
	ctx := t.Context()
	provider, err := NewTracerProvider(ctx, "test", "test", "test")
	if err != nil {
		t.Fatalf("NewTracerProvider() error = %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := provider.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
