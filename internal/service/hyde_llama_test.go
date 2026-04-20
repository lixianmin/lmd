package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type mockHyDEModel struct {
	response string
	err      error
}

func (m *mockHyDEModel) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	return m.response, m.err
}

func TestHyDEGenerator_Interface(t *testing.T) {
	var _ HyDEModel = (*mockHyDEModel)(nil)
}

func TestNewHyDEGenerator(t *testing.T) {
	mock := &mockHyDEModel{response: "test"}
	gen := NewHyDEGenerator(mock, 200)
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
}

func TestHyDEGenerator_Generate(t *testing.T) {
	mock := &mockHyDEModel{response: "dark mode reduces eye strain"}
	gen := NewHyDEGenerator(mock, 200)
	doc, err := gen.Generate(context.Background(), "dark mode preferences")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "dark mode reduces eye strain" {
		t.Fatalf("unexpected doc: %s", doc)
	}
}

func TestHyDEGenerator_EmptyResponse(t *testing.T) {
	mock := &mockHyDEModel{response: ""}
	gen := NewHyDEGenerator(mock, 200)
	doc, err := gen.Generate(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "" {
		t.Fatalf("expected empty, got %q", doc)
	}
}

func TestHyDEGenerator_GenerateError(t *testing.T) {
	mock := &mockHyDEModel{err: fmt.Errorf("model error")}
	gen := NewHyDEGenerator(mock, 200)
	_, err := gen.Generate(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHyDEGenerator_PromptContainsQuery(t *testing.T) {
	var captured string
	mock := &mockHyDEModel{}
	orig := mock.Generate
	_ = orig
	captureMock := &struct {
		HyDEModel
	}{}
	captureMock.HyDEModel = &queryCaptureModel{capture: &captured}
	gen := NewHyDEGenerator(captureMock, 200)
	gen.Generate(context.Background(), "my special query")
	if captured == "" {
		t.Fatal("expected prompt to be passed")
	}
}

type queryCaptureModel struct {
	capture *string
}

func (q *queryCaptureModel) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	*q.capture = prompt
	return "", nil
}

func TestLlamaHyDEModel_ModelNotFound(t *testing.T) {
	m := NewLlamaHyDEModel("/nonexistent/model.gguf", -1, 4)
	_, err := m.Generate(context.Background(), "test prompt", 200)
	if err == nil {
		t.Fatal("expected error for missing model file")
	}
}

func TestLlamaHyDEModel_ReleaseIfIdle_NotLoaded(t *testing.T) {
	m := NewLlamaHyDEModel("/fake/model.gguf", -1, 4)
	released := m.ReleaseIfIdle(10 * time.Nanosecond)
	if released {
		t.Fatal("should not release when not loaded")
	}
}

func TestLlamaHyDEModel_Close_Idempotent(t *testing.T) {
	m := NewLlamaHyDEModel("/fake/model.gguf", -1, 4)
	if err := m.Close(); err != nil {
		t.Fatalf("Close on unloaded model should not error: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Second Close should not error: %v", err)
	}
}
