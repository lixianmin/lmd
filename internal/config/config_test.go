package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Daemon.Port != 12345 {
		t.Fatalf("expected port 12345, got %d", cfg.Daemon.Port)
	}

	if cfg.Embedding.Provider != "ollama" {
		t.Fatalf("expected embedding provider ollama, got %s", cfg.Embedding.Provider)
	}
	if cfg.Embedding.Model != "batiai/qwen3-embedding:0.6b" {
		t.Fatalf("expected embedding model batiai/qwen3-embedding:0.6b, got %s", cfg.Embedding.Model)
	}
	if cfg.Embedding.BatchSize != 8 {
		t.Fatalf("expected embedding batch_size 8, got %d", cfg.Embedding.BatchSize)
	}

	if cfg.Hyde.Provider != "siliconflow" {
		t.Fatalf("expected hyde provider siliconflow, got %s", cfg.Hyde.Provider)
	}
	if cfg.Hyde.Model != "Qwen/Qwen2.5-7B-Instruct" {
		t.Fatalf("expected hyde model Qwen/Qwen2.5-7B-Instruct, got %s", cfg.Hyde.Model)
	}
	if cfg.Hyde.MaxOutputTokens != 768 {
		t.Fatalf("expected hyde max_output_tokens 768, got %d", cfg.Hyde.MaxOutputTokens)
	}
	if cfg.Hyde.MaxInputTokens != 30000 {
		t.Fatalf("expected hyde max_input_tokens 30000, got %d", cfg.Hyde.MaxInputTokens)
	}
	if !cfg.Hyde.NoThinking {
		t.Fatal("expected hyde no_thinking true")
	}

	if cfg.Providers.Ollama.BaseURL != "http://localhost:11434" {
		t.Fatalf("expected ollama base_url, got %s", cfg.Providers.Ollama.BaseURL)
	}
	if cfg.Providers.SiliconFlow.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("expected siliconflow base_url, got %s", cfg.Providers.SiliconFlow.BaseURL)
	}
	if cfg.Providers.DeepSeek.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("expected deepseek base_url, got %s", cfg.Providers.DeepSeek.BaseURL)
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
	if err := yaml.Unmarshal(data, &loaded); err != nil {
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

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("daemon:\n    port: 19999\n"), 0644)

	Load()
	if Cfg.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", Cfg.Daemon.Port)
	}
	if Cfg.Embedding.Provider != "ollama" {
		t.Fatalf("expected default embedding provider ollama, got %s", Cfg.Embedding.Provider)
	}
	if Cfg.Hyde.Provider != "siliconflow" {
		t.Fatalf("expected default hyde provider siliconflow, got %s", Cfg.Hyde.Provider)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()

	envContent := "SILICONFLOW_API_KEY=sk-test-from-dotenv\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	env := loadDotEnv(dir)
	if env["SILICONFLOW_API_KEY"] != "sk-test-from-dotenv" {
		t.Fatalf("expected sk-test-from-dotenv, got %s", env["SILICONFLOW_API_KEY"])
	}
}

func TestLoadDotEnvIgnoresCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()

	envContent := "# comment\n\nFOO=bar\n#another\nBAZ=qux\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	env := loadDotEnv(dir)
	if len(env) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(env))
	}
	if env["FOO"] != "bar" {
		t.Fatalf("expected bar, got %s", env["FOO"])
	}
	if env["BAZ"] != "qux" {
		t.Fatalf("expected qux, got %s", env["BAZ"])
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	dir := t.TempDir()
	env := loadDotEnv(dir)
	if len(env) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(env))
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	configDir = dir
	Reset()
	defer func() {
		configDir = origDir
		Reset()
		os.Unsetenv("SILICONFLOW_API_KEY")
	}()

	os.Setenv("SILICONFLOW_API_KEY", "sk-from-env")

	configContent := "providers:\n    siliconflow:\n        base_url: https://api.siliconflow.cn/v1\n        api_key: sk-from-config\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0644)

	Load()

	if Cfg.Providers.SiliconFlow.APIKey != "sk-from-env" {
		t.Fatalf("expected sk-from-env (env override), got %s", Cfg.Providers.SiliconFlow.APIKey)
	}
}

func TestDotEnvOverridesConfigWhenNoEnvSet(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	origEnvDir := envDir
	configDir = dir
	envDir = dir
	Reset()
	defer func() {
		configDir = origDir
		envDir = origEnvDir
		Reset()
		os.Unsetenv("SILICONFLOW_API_KEY")
	}()

	os.Unsetenv("SILICONFLOW_API_KEY")

	dotEnvContent := "SILICONFLOW_API_KEY=sk-from-dotenv\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(dotEnvContent), 0644)

	configContent := "providers:\n    siliconflow:\n        base_url: https://api.siliconflow.cn/v1\n        api_key: sk-from-config\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0644)

	Load()

	if Cfg.Providers.SiliconFlow.APIKey != "sk-from-dotenv" {
		t.Fatalf("expected sk-from-dotenv (.env override), got %s", Cfg.Providers.SiliconFlow.APIKey)
	}
}

func TestEnvTakesPrecedenceOverDotEnv(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	origEnvDir := envDir
	configDir = dir
	envDir = dir
	Reset()
	defer func() {
		configDir = origDir
		envDir = origEnvDir
		Reset()
		os.Unsetenv("SILICONFLOW_API_KEY")
	}()

	os.Setenv("SILICONFLOW_API_KEY", "sk-from-env")

	dotEnvContent := "SILICONFLOW_API_KEY=sk-from-dotenv\n"
	os.WriteFile(filepath.Join(dir, ".env"), []byte(dotEnvContent), 0644)

	configContent := "providers:\n    siliconflow:\n        base_url: https://api.siliconflow.cn/v1\n        api_key: sk-from-config\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0644)

	Load()

	if Cfg.Providers.SiliconFlow.APIKey != "sk-from-env" {
		t.Fatalf("expected sk-from-env (env takes precedence), got %s", Cfg.Providers.SiliconFlow.APIKey)
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
