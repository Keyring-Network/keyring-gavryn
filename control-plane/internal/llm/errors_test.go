package llm

import (
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		expected string
	}{
		{
			name:     "unsupported provider - openai",
			provider: "openai",
			expected: "unsupported LLM provider: openai",
		},
		{
			name:     "unsupported provider - codex",
			provider: "codex",
			expected: "unsupported LLM provider: codex",
		},
		{
			name:     "unsupported provider - empty",
			provider: "",
			expected: "unsupported LLM provider: ",
		},
		{
			name:     "unsupported provider - custom",
			provider: "my-custom-provider",
			expected: "unsupported LLM provider: my-custom-provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ErrUnsupportedProvider{Provider: tt.provider}
			if err.Error() != tt.expected {
				t.Errorf("expected error message '%s', got '%s'", tt.expected, err.Error())
			}
		})
	}
}

func TestErrUnsupportedProvider_Type(t *testing.T) {
	err := ErrUnsupportedProvider{Provider: "test"}

	// Verify it's the correct type
	var _ error = err

	// Verify the struct field is accessible
	if err.Provider != "test" {
		t.Errorf("expected Provider field to be 'test', got '%s'", err.Provider)
	}
}
