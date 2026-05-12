package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lixianmin/logo"
	"gopkg.in/yaml.v3"
)

var Cfg *Config
var once sync.Once
var configDir string
var envDir string

type Config struct {
	Providers ProviderConfig  `yaml:"providers"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Hyde HydeConfig `yaml:"hyde"`
	Database  DatabaseConfig  `yaml:"database"`
	Daemon    DaemonConfig    `yaml:"daemon"`
}

type DaemonConfig struct {
	Port int `yaml:"port"`
}

type ProviderConfig struct {
	Ollama      ProviderItem `yaml:"ollama"`
	SiliconFlow ProviderItem `yaml:"siliconflow"`
	DeepSeek    ProviderItem `yaml:"deepseek"`
}

type ProviderItem struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key,omitempty"`
}

type EmbeddingConfig struct {
	Provider    string `yaml:"provider"`
	Model       string `yaml:"model"`
	QueryPrefix string `yaml:"query_prefix"`
	BatchSize   int    `yaml:"batch_size"`
}

type HydeConfig struct {
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	MaxInputTokens  int    `yaml:"max_input_tokens"`
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
				BaseURL: "http://localhost:11434",
			},
			SiliconFlow: ProviderItem{
				BaseURL: "https://api.siliconflow.cn/v1",
				APIKey: "sk-your-api-key-here",
			},
			DeepSeek: ProviderItem{
				BaseURL: "https://api.deepseek.com/v1",
			},
		},
		Embedding: EmbeddingConfig{
			Provider:    "ollama",
			Model:       "batiai/qwen3-embedding:0.6b",
			QueryPrefix: "Instruct: Given a document query, retrieve the most relevant chunk.\nQuery: ",
			BatchSize:   8,
		},
		Hyde: HydeConfig{
			Provider:        "siliconflow",
			Model:           "Qwen/Qwen2.5-7B-Instruct",
			MaxOutputTokens: 768,
			MaxInputTokens:  30000,
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

		loadDotEnvToEnv(resolveEnvDir())

		configPath := resolveConfigPath()
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				_ = SaveDefault(configPath)
				logo.Info("config: created default config at %s", configPath)
			} else {
				logo.Warn("config: read %s error: %s, using defaults", configPath, err)
			}
		} else if err := yaml.Unmarshal(data, Cfg); err != nil {
			logo.Warn("config: unmarshal error: %s, using defaults", err)
		}

		overrideFromEnv()
	})
}

func resolveEnvDir() string {
	if envDir != "" {
		return envDir
	}
	dir, _ := os.Getwd()
	return dir
}

func loadDotEnvToEnv(dir string) {
	env := loadDotEnv(dir)
	for k, v := range env {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func overrideFromEnv() {
	if v := os.Getenv("SILICONFLOW_API_KEY"); v != "" {
		Cfg.Providers.SiliconFlow.APIKey = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		Cfg.Providers.DeepSeek.APIKey = v
	}
}

func loadDotEnv(dir string) map[string]string {
	result := make(map[string]string)
	path := filepath.Join(dir, ".env")
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		result[key] = value
	}
	return result
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
	data, err := yaml.Marshal(toSave)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600)
}
