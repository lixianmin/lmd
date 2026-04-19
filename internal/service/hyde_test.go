package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateHypotheticalDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			var req struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)

			if len(req.Messages) == 0 {
				t.Error("expected at least one message")
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": map[string]interface{}{
					"content": "This is a hypothetical document about dark mode preferences.",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	doc, err := GenerateHypotheticalDocument(context.Background(), server.URL, "test-model", "dark mode preferences")
	if err != nil {
		t.Fatal(err)
	}
	if doc == "" {
		t.Fatal("expected non-empty document")
	}
	if !strings.Contains(doc, "hypothetical") {
		t.Fatalf("unexpected doc content: %s", doc)
	}
}

func TestGenerateHypotheticalDocumentEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": map[string]interface{}{
					"content": "",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	doc, err := GenerateHypotheticalDocument(context.Background(), server.URL, "test-model", "test query")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "" {
		t.Fatalf("expected empty doc, got %q", doc)
	}
}

func TestGenerateHypotheticalDocumentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	_, err := GenerateHypotheticalDocument(context.Background(), server.URL, "test-model", "test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
