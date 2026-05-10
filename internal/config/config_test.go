package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Daemon.Port != 12345 {
		t.Fatalf("expected port 12345, got %d", cfg.Daemon.Port)
	}

	if cfg.Embedding.Provider != "ollama" {
		t.Fatalf("expected embedding provider ollama, got %s", cfg.Embedding.Provider)
	}
	if cfg.Embedding.Model != "batiai/qwen3-embedding" {
		t.Fatalf("expected embedding model batiAI/qwen3-embedding, got %s", cfg.Embedding.Model)
	}
	if cfg.Embedding.BatchSize != 8 {
		t.Fatalf("expected embedding batch_size 8, got %d", cfg.Embedding.BatchSize)
	}

	if cfg.Summary.Provider != "ollama" {
		t.Fatalf("expected summary provider ollama, got %s", cfg.Summary.Provider)
	}
	if cfg.Summary.Model != "qwen3.5" {
		t.Fatalf("expected summary model qwen3.5, got %s", cfg.Summary.Model)
	}
	if cfg.Summary.MaxOutputTokens != 512 {
		t.Fatalf("expected summary max_output_tokens 512, got %d", cfg.Summary.MaxOutputTokens)
	}
	if cfg.Summary.MaxInputTokens != 245000 {
		t.Fatalf("expected summary max_input_tokens 245000, got %d", cfg.Summary.MaxInputTokens)
	}
	if cfg.Summary.CooldownSeconds != 120 {
		t.Fatalf("expected summary cooldown_seconds 120, got %d", cfg.Summary.CooldownSeconds)
	}
	if !cfg.Summary.NoThinking {
		t.Fatal("expected summary no_thinking true")
	}

	if cfg.Providers.Ollama.URL != "http://localhost:11434" {
		t.Fatalf("expected ollama url, got %s", cfg.Providers.Ollama.URL)
	}
	if cfg.Providers.SiliconFlow.URL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("expected siliconflow url, got %s", cfg.Providers.SiliconFlow.URL)
	}

	if cfg.Database.Path == "" {
		t.Fatal("expected non-empty database path")
	}
	if filepath.Base(cfg.Database.Path) != "index.sqlite" {
		t.Fatalf("expected index.sqlite as database file, got %s", filepath.Base(cfg.Database.Path))
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	Reset()
	defer func() {
		configDir = orig
		Reset()
	}()

	cfg := DefaultConfig()
	cfg.Daemon.Port = 19999
	Cfg = cfg

	if err := SaveDefault(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// read back the saved JSON and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", loaded.Daemon.Port)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	Reset()
	defer func() {
		configDir = orig
		Reset()
	}()

	Load()
	if Cfg == nil {
		t.Fatal("Cfg should not be nil after Load()")
	}
	if Cfg.Daemon.Port != 12345 {
		t.Fatalf("expected default port 12345, got %d", Cfg.Daemon.Port)
	}
}

func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	Reset()
	defer func() {
		configDir = orig
		Reset()
	}()

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`{"daemon":{"port":19999}}`), 0644)

	Load()
	if Cfg.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", Cfg.Daemon.Port)
	}
	if Cfg.Embedding.Provider != "ollama" {
		t.Fatalf("expected default embedding provider ollama, got %s", Cfg.Embedding.Provider)
	}
	if Cfg.Summary.Provider != "ollama" {
		t.Fatalf("expected default summary provider ollama, got %s", Cfg.Summary.Provider)
	}
}

func TestLoadAutoGeneratesFile(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	Reset()
	defer func() {
		configDir = orig
		Reset()
	}()

	Load()

	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file should be auto-generated on first load")
	}
}
