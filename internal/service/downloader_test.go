package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadModel_FileExists(t *testing.T) {
	dir := t.TempDir()
	fakeModel := filepath.Join(dir, "model.gguf")
	os.WriteFile(fakeModel, []byte("fake"), 0644)

	err := DownloadModel(fakeModel, "https://example.com/model.gguf")
	if err != nil {
		t.Fatalf("should not download when file exists: %v", err)
	}
}

func TestDownloadModel_DownloadSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("gguf-model-data"))
	}))
	defer server.Close()

	err := DownloadModel(target, server.URL+"/model.gguf")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != "gguf-model-data" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestDownloadModel_MirrorFallback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	callCount := 0
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("from-mirror"))
	}))
	defer goodServer.Close()

	err := DownloadModel(target, badServer.URL+"/fail", goodServer.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != "from-mirror" {
		t.Fatalf("expected mirror content, got %s", string(data))
	}
	if callCount != 1 {
		t.Fatalf("expected primary to be tried once, got %d calls", callCount)
	}
}

func TestDownloadModel_AllFail(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	err := DownloadModel(target, "http://127.0.0.1:1/fail1", "http://127.0.0.1:1/fail2")
	if err == nil {
		t.Fatal("expected error when all downloads fail")
	}
}

func TestDownloadModel_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "subdir", "nested", "model.gguf")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer server.Close()

	err := DownloadModel(target, server.URL+"/model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Fatal("file should exist after download")
	}
}

func TestDownloadModel_NoTmpFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()

	_ = DownloadModel(target, badServer.URL+"/fail")
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp file should be cleaned up on failure")
	}
}
