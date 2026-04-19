package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Daemon    DaemonConfig    `yaml:"daemon"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Vector    VectorConfig    `yaml:"vector"`
	Database  DatabaseConfig  `yaml:"database"`
	HyDE      HyDEConfig      `yaml:"hyde"`
}

type DaemonConfig struct {
	Port              int    `yaml:"port"`
	IdleTimeout       string `yaml:"idle_timeout"`
	IndexPollInterval string `yaml:"index_poll_interval"`
}

type EmbeddingConfig struct {
	Provider   string       `yaml:"provider"`
	Ollama     OllamaConfig `yaml:"ollama"`
	BatchSize  int          `yaml:"batch_size"`
	Truncation int          `yaml:"truncation"`
}

type OllamaConfig struct {
	URL       string `yaml:"url"`
	Model     string `yaml:"model"`
	KeepAlive string `yaml:"keep_alive"`
}

type VectorConfig struct {
	Dimensions     int    `yaml:"dimensions"`
	DistanceMetric string `yaml:"distance_metric"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type HyDEConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
}

var Cfg *Config

var configDir string

func init() {
	home, _ := os.UserHomeDir()
	configDir = filepath.Join(home, ".config", "lmd")
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Daemon: DaemonConfig{
			Port:              18200,
			IdleTimeout:       "30m",
			IndexPollInterval: "30s",
		},
		Embedding: EmbeddingConfig{
			Provider: "ollama",
			Ollama: OllamaConfig{
				URL:       "http://localhost:11434",
				Model:     "qwen3-embedding:0.6b-q8_0",
				KeepAlive: "30m",
			},
			BatchSize:  8,
			Truncation: 800,
		},
		Vector: VectorConfig{
			Dimensions:     1024,
			DistanceMetric: "cosine",
		},
		Database: DatabaseConfig{
			Path: filepath.Join(home, ".cache", "lmd", "index.sqlite"),
		},
		HyDE: HyDEConfig{
			Enabled: true,
			Model:   "qwen3:0.6b-q8_0",
		},
	}
}

func Load() (*Config, error) {
	path := filepath.Join(configDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			Cfg = cfg
			return cfg, nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	Cfg = cfg
	return cfg, nil
}

func SaveDefault() error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	toSave := Cfg
	if toSave == nil {
		toSave = DefaultConfig()
	}
	data, err := yaml.Marshal(toSave)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "config.yaml"), data, 0644)
}
