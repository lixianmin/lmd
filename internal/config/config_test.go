package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Daemon.Port != 18200 {
		t.Fatalf("expected port 18200, got %d", cfg.Daemon.Port)
	}
	if cfg.Embedding.Ollama.URL != "http://localhost:11434" {
		t.Fatalf("unexpected ollama url: %s", cfg.Embedding.Ollama.URL)
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
	if cfg.Daemon.IndexPollInterval != "60s" {
		t.Fatalf("expected index_poll_interval 60s, got %s", cfg.Daemon.IndexPollInterval)
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
	if loaded.Daemon.Port != 18200 {
		t.Fatalf("expected default port 18200, got %d", loaded.Daemon.Port)
	}
}
