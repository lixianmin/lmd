package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/logo"
)

var Cfg *Config
var once sync.Once
var configDir string // test hook: overrides default config directory

type Config struct {
	Providers ProviderConfig  `yaml:"providers"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Summary   SummaryConfig   `yaml:"summary"`
	Database  DatabaseConfig  `yaml:"database"`
	Daemon    DaemonConfig    `yaml:"daemon"`
}

type DaemonConfig struct {
	Port int `yaml:"port"`
}

type ProviderConfig struct {
	Ollama      ProviderItem `yaml:"ollama"`
	SiliconFlow ProviderItem `yaml:"siliconflow"`
}

type ProviderItem struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key,omitempty"`
}

type EmbeddingConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BatchSize int    `yaml:"batch_size"`
}

type SummaryConfig struct {
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	MaxInputTokens  int    `yaml:"max_input_tokens"`
	CooldownSeconds int    `yaml:"cooldown_seconds"`
	NoThinking      bool   `yaml:"no_thinking"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

func DefaultConfig() *Config {
	return &Config{
		Daemon: DaemonConfig{
			Port: 12345,
		},
		Providers: ProviderConfig{
			Ollama: ProviderItem{
				URL: "http://localhost:11434",
			},
			SiliconFlow: ProviderItem{
				URL:    "https://api.siliconflow.cn/v1",
				APIKey: "sk-your-api-key-here",
			},
		},
		Embedding: EmbeddingConfig{
			Provider:  "ollama",
			Model:     "batiai/qwen3-embedding:0.6b",
			BatchSize: 8,
		},
		Summary: SummaryConfig{
			Provider:        "ollama",
			Model:           "qwen3.5",
			MaxOutputTokens: 512,
			MaxInputTokens:  245000,
			CooldownSeconds: 120,
			NoThinking:      true,
		},
		Database: DatabaseConfig{
			Path: filepath.Join(os.Getenv("HOME"), ".cache", "lmd", "index.sqlite"),
		},
	}
}

func Load() {
	once.Do(func() {
		Cfg = DefaultConfig()
		configPath := resolveConfigPath()
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				_ = SaveDefault(configPath)
				logo.Info("config: created default config at %s", configPath)
				return
			}
			logo.Warn("config: read %s error: %s, using defaults", configPath, err)
			return
		}
		if err := convert.FromJsonE(data, Cfg); err != nil {
			logo.Warn("config: unmarshal error: %s, using defaults", err)
			return
		}
	})
}

func resolveConfigPath() string {
	if configDir != "" {
		return filepath.Join(configDir, "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lmd", "config.yaml")
}

func Reset() {
	once = sync.Once{}
	Cfg = nil
}

func SaveDefault(configPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	toSave := Cfg
	if toSave == nil {
		toSave = DefaultConfig()
	}
	data, err := convert.ToJsonE(toSave)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}
