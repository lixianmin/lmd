package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_IsAlive_ReturnsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	if !c.IsAlive() {
		t.Fatal("expected IsAlive true")
	}
}

func TestClient_IsAlive_ReturnsFalse(t *testing.T) {
	c := NewClient(19999)
	if c.IsAlive() {
		t.Fatal("expected IsAlive false for unreachable port")
	}
}

func TestClient_Post_SendsJSON(t *testing.T) {
	var receivedBody map[string]interface{}
	var receivedMethod string
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}

	body := map[string]interface{}{
		"query":      "test query",
		"collection": "notes",
		"limit":      5,
	}

	resp, err := c.Post("/search", body)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	if receivedMethod != "POST" {
		t.Fatalf("expected POST, got %s", receivedMethod)
	}
	if receivedPath != "/search" {
		t.Fatalf("expected /search, got %s", receivedPath)
	}
	if receivedBody["query"] != "test query" {
		t.Fatalf("expected query 'test query', got %v", receivedBody["query"])
	}

	var result map[string]string
	json.Unmarshal(resp, &result)
	if result["result"] != "ok" {
		t.Fatalf("expected result ok, got %v", result["result"])
	}
}

func TestClient_Get(t *testing.T) {
	var receivedMethod string
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	resp, err := c.Get("/health")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if receivedMethod != "GET" {
		t.Fatalf("expected GET, got %s", receivedMethod)
	}
	if receivedPath != "/health" {
		t.Fatalf("expected /health, got %s", receivedPath)
	}

	var result map[string]string
	json.Unmarshal(resp, &result)
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", result["status"])
	}
}

func TestClient_Search_SendsCorrectBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["query"] != "hello" {
			t.Fatalf("expected query 'hello', got %v", body["query"])
		}
		if body["collection"] != "docs" {
			t.Fatalf("expected collection 'docs', got %v", body["collection"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"hits": []interface{}{}})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.Search("hello", "docs", 5, 0, "text", false)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
}

func TestClient_CollectionList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/collection/list" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.CollectionList()
	if err != nil {
		t.Fatalf("CollectionList failed: %v", err)
	}
}

func TestClient_Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/status" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"total": 0})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
}

func TestClient_Rebuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/rebuild" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"indexed": 0})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
}

func TestClient_CollectionAdd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/collection/add" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test" {
			t.Fatalf("expected name 'test', got %v", body["name"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.CollectionAdd("/tmp", "test", "**/*.md")
	if err != nil {
		t.Fatalf("CollectionAdd failed: %v", err)
	}
}

func TestClient_CollectionRemove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/collection/remove" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "old" {
			t.Fatalf("expected name 'old', got %v", body["name"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.CollectionRemove("old")
	if err != nil {
		t.Fatalf("CollectionRemove failed: %v", err)
	}
}

func TestClient_CollectionRename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/collection/rename" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["old"] != "a" || body["new"] != "b" {
			t.Fatalf("expected old=a new=b, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.CollectionRename("a", "b")
	if err != nil {
		t.Fatalf("CollectionRename failed: %v", err)
	}
}

func TestClient_VSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/vsearch" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "vec query" {
			t.Fatalf("expected query 'vec query', got %v", body["query"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"hits": []interface{}{}})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.VSearch("vec query", "", 5, 0.3)
	if err != nil {
		t.Fatalf("VSearch failed: %v", err)
	}
}

func TestClient_Query(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/query" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"hits": []interface{}{}})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.Hybrid("hybrid", "col", 10, 0.5)
	if err != nil {
		t.Fatalf("Hybrid failed: %v", err)
	}
}

func TestClient_GetDoc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/get" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["path"] != "notes/test.md" {
			t.Fatalf("expected path 'notes/test.md', got %v", body["path"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"title": "Test"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, client: srv.Client()}
	_, err := c.GetDoc("notes/test.md", true, 0, 0)
	if err != nil {
		t.Fatalf("GetDoc failed: %v", err)
	}
}
