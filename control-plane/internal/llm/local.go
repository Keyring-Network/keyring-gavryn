package llm

import (
	"context"
	"errors"
)

type LocalProvider struct{}

func (LocalProvider) Generate(ctx context.Context, messages []Message) (string, error) {
	return "", errors.New("local LLM mode is not implemented")
}
