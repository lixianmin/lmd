package llm

import "context"

type Message struct {
	Role    string
	Content string
}

type LLMProvider interface {
	ChatCompletion(ctx context.Context, messages []Message) (string, error)
	Close() error
}
