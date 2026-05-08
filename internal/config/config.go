package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Daemon    DaemonConfig    `yaml:"daemon"`
	Llama     LlamaConfig     `yaml:"llama"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	HyDE      HyDEConfig      `yaml:"hyde"`
	Vector    VectorConfig    `yaml:"vector"`
	Database  DatabaseConfig  `yaml:"database"`
	Topic     TopicConfig     `yaml:"topic"`
}

type DaemonConfig struct {
	Port int `yaml:"port"`
}

type LlamaConfig struct {
	EmbedModel       string `yaml:"embed_model"`
	GPULayers        int    `yaml:"gpu_layers"`
	ModelIdleTimeout string `yaml:"model_idle_timeout"`
	Parallel         int    `yaml:"parallel"`
	Threads          int    `yaml:"threads"`
}

type EmbeddingConfig struct {
	BatchSize int `yaml:"batch_size"`
	// 发送给 embedding 模型前，每个 chunk 文本的 rune 截断上限。
	// 必须大于 chunker.hardMax (= chunkSize + chunkSize/2 = 450)，否则 overlap 拼接后的大 chunk 会被截断导致 embedding 丢信息。
	Truncation int `yaml:"truncation"`
}

type HyDEConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	Model     string `yaml:"model"`
	MaxTokens int    `yaml:"max_tokens"`
}

type VectorConfig struct {
	Dimensions     int    `yaml:"dimensions"`
	DistanceMetric string `yaml:"distance_metric"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type TopicConfig struct {
	SummarizeModel     string `yaml:"summarize_model"`
	SummarizeGPULayers int    `yaml:"summarize_gpu_layers"`
	SummarizeThreads   int    `yaml:"summarize_threads"`
	CooldownSeconds    int    `yaml:"cooldown_seconds"`
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
			Port: 12345,
		},
		Llama: LlamaConfig{
			EmbedModel:       filepath.Join(home, ".cache", "lmd", "models", "Qwen3-Embedding-0.6B-Q8_0.gguf"),
			GPULayers:        -1,
			ModelIdleTimeout: "10m",
			Parallel:         8,
			Threads:          4,
		},
		Embedding: EmbeddingConfig{
			BatchSize:  8,
			Truncation: 500,
		},
		HyDE: HyDEConfig{
			BaseURL:   "https://api.siliconflow.cn/v1",
			APIKey:    "",
			Model:     "Qwen/Qwen3.5-9B",
			MaxTokens: 200,
		},
		Vector: VectorConfig{
			Dimensions:     1024,
			DistanceMetric: "cosine",
		},
		Database: DatabaseConfig{
			Path: filepath.Join(home, ".cache", "lmd", "index.sqlite"),
		},
		Topic: TopicConfig{
			SummarizeModel:     filepath.Join(home, ".cache", "lmd", "models", "Qwen3-4B-Instruct-2507-Q4_K_M.gguf"),
			SummarizeGPULayers: -1,
			SummarizeThreads:   4,
			CooldownSeconds:    300,
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
			SaveDefault()
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
	return os.WriteFile(filepath.Join(configDir, "config.yaml"), data, 0600)
}
