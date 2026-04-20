package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Daemon.Port != 12345 {
		t.Fatalf("expected port 12345, got %d", cfg.Daemon.Port)
	}
	if cfg.Llama.EmbedModel == "" {
		t.Fatal("expected non-empty llama.embed_model")
	}
	if cfg.Llama.HydeModel == "" {
		t.Fatal("expected non-empty llama.hyde_model")
	}
	if cfg.Llama.GPULayers != -1 {
		t.Fatalf("expected gpu_layers -1, got %d", cfg.Llama.GPULayers)
	}
	if cfg.Llama.Parallel != 8 {
		t.Fatalf("expected parallel 8, got %d", cfg.Llama.Parallel)
	}
	if cfg.Llama.Threads != 4 {
		t.Fatalf("expected threads 4, got %d", cfg.Llama.Threads)
	}
	if cfg.Embedding.BatchSize != 8 {
		t.Fatalf("expected batch_size 8, got %d", cfg.Embedding.BatchSize)
	}
	if cfg.Vector.Dimensions != 1024 {
		t.Fatalf("expected dimensions 1024, got %d", cfg.Vector.Dimensions)
	}
	if cfg.Embedding.Truncation != 800 {
		t.Fatalf("expected truncation 800, got %d", cfg.Embedding.Truncation)
	}
	if cfg.Daemon.IdleTimeout != "30m" {
		t.Fatalf("expected idle_timeout 30m, got %s", cfg.Daemon.IdleTimeout)
	}
	if cfg.Daemon.IndexPollInterval != "30s" {
		t.Fatalf("expected index_poll_interval 30s, got %s", cfg.Daemon.IndexPollInterval)
	}
	if cfg.Llama.ModelIdleTimeout != "10m" {
		t.Fatalf("expected model_idle_timeout 10m, got %s", cfg.Llama.ModelIdleTimeout)
	}
	if !cfg.HyDE.Enabled {
		t.Fatal("expected default HyDE enabled=true")
	}
	if cfg.HyDE.MaxTokens != 200 {
		t.Fatalf("expected hyde max_tokens 200, got %d", cfg.HyDE.MaxTokens)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	defer func() { configDir = orig }()

	cfg := DefaultConfig()
	cfg.Daemon.Port = 19999
	Cfg = cfg

	if err := SaveDefault(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	loaded, err := Load()
	if err != nil {
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
	defer func() { configDir = orig }()

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 12345 {
		t.Fatalf("expected default port 12345, got %d", loaded.Daemon.Port)
	}
}

func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	defer func() { configDir = orig }()

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("daemon:\n  port: 19999\n"), 0644)

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", loaded.Daemon.Port)
	}
	if loaded.Llama.GPULayers != -1 {
		t.Fatalf("expected default gpu_layers -1, got %d", loaded.Llama.GPULayers)
	}
	if loaded.Embedding.BatchSize != 8 {
		t.Fatalf("expected default batch_size 8, got %d", loaded.Embedding.BatchSize)
	}
	if !loaded.HyDE.Enabled {
		t.Fatal("expected default HyDE enabled=true")
	}
}

func TestLoadAutoGeneratesFile(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	defer func() { configDir = orig }()

	_, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file should be auto-generated on first load")
	}
}
