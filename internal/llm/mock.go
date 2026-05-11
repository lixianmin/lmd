package llm

import "context"

type MockLLM struct {
	Response string
	Err      error
	Called   int
	LastMsgs []Message
}

func NewMockLLM(response string) *MockLLM {
	return &MockLLM{Response: response}
}

func (my *MockLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	my.Called++
	my.LastMsgs = messages
	if my.Err != nil {
		return "", my.Err
	}
	return my.Response, nil
}

func (my *MockLLM) Close() error { return nil }
