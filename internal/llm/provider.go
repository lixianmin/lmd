package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMProvider interface {
	ChatCompletion(ctx context.Context, messages []Message) (string, error)
	Close() error
}
