package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHyDEAPIClientGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Fatalf("expected 1 user message, got %v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "Docker volumes enable persistent data storage across container restarts.",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewHyDEAPIClient(server.URL+"/v1", "test-key", "Qwen/Qwen3.5-9B", 200)
	doc, err := client.Generate(context.Background(), "docker volume mount")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Docker volumes") {
		t.Fatalf("unexpected doc: %s", doc)
	}
}

func TestHyDEAPIClientNoAPIKey(t *testing.T) {
	client := NewHyDEAPIClient("https://api.example.com/v1", "", "model", 200)
	_, err := client.Generate(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("expected api_key error, got: %s", err)
	}
}

func TestHyDEAPIClientHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewHyDEAPIClient(server.URL+"/v1", "key", "model", 200)
	_, err := client.Generate(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHyDEAPIClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := NewHyDEAPIClient(server.URL+"/v1", "key", "model", 200)
	_, err := client.Generate(ctx, "test")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHyDEAPIClientTrimsOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "  hello world  \n",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewHyDEAPIClient(server.URL+"/v1", "key", "model", 200)
	doc, err := client.Generate(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "hello world" {
		t.Fatalf("expected trimmed output, got %q", doc)
	}
}
