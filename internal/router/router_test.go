package router

import (
	"fmt"
	"testing"

	"github.com/yewintnaing/ai-gateway/internal/config"
)

func TestRouter_Route(t *testing.T) {
	routes := []config.Route{
		{
			Name:  "support",
			Match: config.Match{UseCase: "support_summary"},
			Primary: config.Target{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
			Fallbacks: []config.Target{
				{Provider: "anthropic", Model: "claude-3-sonnet"},
			},
		},
		{
			Name:  "default",
			Match: config.Match{UseCase: "default"},
			Primary: config.Target{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
	}

	r := NewRouter(routes)

	t.Run("Match use case", func(t *testing.T) {
		rt := r.Route("support_summary")
		if rt.Name != "support" {
			t.Errorf("expected support, got %s", rt.Name)
		}
		if rt.Primary.Provider != "openai" {
			t.Errorf("expected openai provider, got %s", rt.Primary.Provider)
		}
	})

	t.Run("Fallback to default", func(t *testing.T) {
		rt := r.Route("unknown")
		if rt.Name != "default" {
			t.Errorf("expected default, got %s", rt.Name)
		}
	})
}

func TestIsRetryable(t *testing.T) {
	// Test StatusCodeIsRetryable
	if !StatusCodeIsRetryable(500) {
		t.Error("expected 500 to be retryable")
	}
	if !StatusCodeIsRetryable(429) {
		t.Error("expected 429 to be retryable")
	}
	if StatusCodeIsRetryable(400) {
		t.Error("expected 400 to NOT be retryable")
	}

	// Test IsRetryable - only network errors are retryable
	t.Run("Non-network errors should not be retryable", func(t *testing.T) {
		err := fmt.Errorf("anthropic API error: 400 - insufficient credits")
		if IsRetryable(err) {
			t.Error("expected API error to NOT be retryable")
		}
	})

	t.Run("Unknown errors should not be retryable", func(t *testing.T) {
		err := fmt.Errorf("some unknown error")
		if IsRetryable(err) {
			t.Error("expected unknown error to NOT be retryable")
		}
	})
}
