package llm

import (
	"context"
	"errors"
	"testing"
)

func TestGenerate_Local(t *testing.T) {
	provider := LocalProvider{}

	messages := []Message{
		{Role: "user", Content: "Hello"},
	}

	result, err := provider.Generate(context.Background(), messages)

	if err == nil {
		t.Fatal("expected error for local provider, got nil")
	}

	if !errors.Is(err, errors.New("local LLM mode is not implemented")) && err.Error() != "local LLM mode is not implemented" {
		t.Errorf("expected 'local LLM mode is not implemented' error, got: %s", err.Error())
	}

	if result != "" {
		t.Errorf("expected empty result, got: %s", result)
	}
}

func TestGenerate_LocalWithContext(t *testing.T) {
	provider := LocalProvider{}

	// Test with cancelled context - should still return the same error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	result, err := provider.Generate(ctx, messages)

	// Local provider doesn't check context, it just returns the unimplemented error
	if err == nil {
		t.Fatal("expected error for local provider, got nil")
	}

	if err.Error() != "local LLM mode is not implemented" {
		t.Errorf("expected 'local LLM mode is not implemented' error, got: %s", err.Error())
	}

	if result != "" {
		t.Errorf("expected empty result, got: %s", result)
	}
}

func TestGenerate_LocalEmptyMessages(t *testing.T) {
	provider := LocalProvider{}

	result, err := provider.Generate(context.Background(), []Message{})

	if err == nil {
		t.Fatal("expected error for local provider, got nil")
	}

	if err.Error() != "local LLM mode is not implemented" {
		t.Errorf("expected 'local LLM mode is not implemented' error, got: %s", err.Error())
	}

	if result != "" {
		t.Errorf("expected empty result, got: %s", result)
	}
}
