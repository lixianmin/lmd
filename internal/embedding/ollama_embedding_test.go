package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"embeddings": [][]float32{{0.1, 0.2, 0.3}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "test-model", "")
	vec, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 dims, got %d", len(vec))
	}
}

func TestOllamaProvider_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		})
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "test-model", "")
	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestOllamaProvider_Dimension(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", "test", "")
	if p.Dimension() != 1024 {
		t.Fatalf("expected 1024, got %d", p.Dimension())
	}
}

func TestOllamaProvider_ModelName(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", "my-model", "")
	if p.ModelName() != "my-model" {
		t.Fatalf("expected my-model, got %s", p.ModelName())
	}
}

func TestOllamaProvider_EmbedQuery(t *testing.T) {
	var receivedInput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input any `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch v := req.Input.(type) {
		case string:
			receivedInput = v
		case []any:
			if len(v) > 0 {
				receivedInput, _ = v[0].(string)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.5}},
		})
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "test", "PREFIX: ")
	_, err := p.EmbedQuery(context.Background(), "docker命令")
	if err != nil {
		t.Fatal(err)
	}

	if receivedInput != "PREFIX: docker命令" {
		t.Fatalf("expected query to have prefix, got: %q", receivedInput)
	}
}

func TestOllamaProvider_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "bad-model", "")
	_, err := p.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
