package llm

import "fmt"

type ErrUnsupportedProvider struct {
	Provider string
}

func (e ErrUnsupportedProvider) Error() string {
	return fmt.Sprintf("unsupported LLM provider: %s", e.Provider)
}
