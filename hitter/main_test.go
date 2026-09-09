package main

import (
	"testing"
	"time"
)

func TestTargetURLUsesResponsesForStream(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{
			name: "non-stream uses configured URL",
			config: Config{
				URL:    "http://localhost:8080/v1/chat/completions",
				Stream: false,
			},
			want: "http://localhost:8080/v1/chat/completions",
		},
		{
			name: "stream keeps configured URL by default",
			config: Config{
				URL:    "http://localhost:8080/v1/chat/completions",
				Stream: true,
			},
			want: "http://localhost:8080/v1/chat/completions",
		},
		{
			name: "responses api stream maps chat completions to responses",
			config: Config{
				URL:          "http://localhost:8080/v1/chat/completions",
				Stream:       true,
				ResponsesAPI: true,
			},
			want: "http://localhost:8080/v1/responses",
		},
		{
			name: "responses api stream keeps responses URL",
			config: Config{
				URL:          "http://localhost:8080/v1/responses",
				Stream:       true,
				ResponsesAPI: true,
			},
			want: "http://localhost:8080/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := targetURL(&tt.config); got != tt.want {
				t.Fatalf("targetURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveMaxConcurrencyAuto(t *testing.T) {
	config := &Config{
		RPS:             100,
		ExpectedLatency: 10 * time.Second,
		MaxConcurrency:  0,
	}

	if got := resolveMaxConcurrency(config); got != 1250 {
		t.Fatalf("resolveMaxConcurrency() = %d, want 1250", got)
	}
}

func TestResolveMaxConcurrencyExplicitAndUnlimited(t *testing.T) {
	explicit := &Config{RPS: 100, ExpectedLatency: 10 * time.Second, MaxConcurrency: 900}
	if got := resolveMaxConcurrency(explicit); got != 900 {
		t.Fatalf("resolveMaxConcurrency(explicit) = %d, want 900", got)
	}

	unlimited := &Config{RPS: 100, ExpectedLatency: 10 * time.Second, MaxConcurrency: -1}
	if got := resolveMaxConcurrency(unlimited); got != 0 {
		t.Fatalf("resolveMaxConcurrency(unlimited) = %d, want 0", got)
	}
}
