> **Superseded by [2026-04-19-daemon-config-ollama-design.md](./2026-04-19-daemon-config-ollama-design.md)** — This spec is now part of the unified daemon architecture spec.

# Config System + Ollama-Only Embedding

## Problem

1. **No configuration file**: Embedding parameters (model name, Ollama URL, batch size, etc.) are hardcoded constants scattered across `internal/embedding/gguf.go`. Users cannot customize anything without editing code.

2. **Unnecessary complexity**: The `GGUFProvider` includes ~200 lines of llama-server subprocess management (process start/stop, PID tracking, watchdog timer, health check polling) as a fallback when Ollama is unavailable. This violates the project principle of "don't reinvent the wheel" — Ollama already handles model loading, serving, and lifecycle.

3. **No graceful degradation**: When embedding is unavailable, commands like `vsearch`/`query`/`embed` fail with obscure errors rather than clear messages.

## Solution

### 1. Config File

**Location**: `~/.config/lmd/config.yaml`

**Auto-generation**: On first run (any `lmd` command), if the file does not exist, generate it with defaults and print a notice to stderr.

**Default content**:

```yaml
# LMD Configuration
# Auto-generated on first run. Edit as needed.

embedding:
  provider: ollama
  ollama:
    url: http://localhost:11434
    model: qwen3-embedding:0.6b-q8_0
    keep_alive: 30m
  batch_size: 8
  truncation: 800

vector:
  dimensions: 1024
  distance_metric: cosine
```

**Config struct**:

```go
type Config struct {
    Embedding EmbeddingConfig `yaml:"embedding"`
    Vector    VectorConfig    `yaml:"vector"`
}

type EmbeddingConfig struct {
    Provider  string       `yaml:"provider"`
    Ollama    OllamaConfig `yaml:"ollama"`
    BatchSize int          `yaml:"batch_size"`
    Truncation int         `yaml:"truncation"`
}

type OllamaConfig struct {
    URL        string `yaml:"url"`
    Model      string `yaml:"model"`
    KeepAlive  string `yaml:"keep_alive"`
}

type VectorConfig struct {
    Dimensions     int    `yaml:"dimensions"`
    DistanceMetric string `yaml:"distance_metric"`
}
```

### 2. Config Package

**Location**: `internal/config/config.go`

Responsibilities:
- `Load() (*Config, error)` — read from `~/.config/lmd/config.yaml`; if missing, call `SaveDefault()` and return defaults
- `SaveDefault() error` — write default config to `~/.config/lmd/config.yaml` (create directory if needed)
- `DefaultConfig() *Config` — return hardcoded defaults
- Global `var Cfg *Config` set during `Load()`

Config is loaded once in `root.go`'s `PersistentPreRunE`, before any command runs.

### 3. Remove llama-server Subprocess

Delete from `internal/embedding/gguf.go`:
- `startLLamaServer()` — subprocess launch
- `watchLLamaServer()` — health check polling
- `startWatchdog()` — idle timeout bash script
- `touchLastActive()` — activity tracking
- `callLlamaEmbed()` — llama-server HTTP endpoint
- PID file (`llama-server.pid`) and last-active file (`llama-server.last-active`) logic
- `EnsureModel()` / `downloadModel()` — HuggingFace model download (Ollama pulls its own models)

### 4. Rename GGUFProvider → OllamaProvider

The provider only supports Ollama now. Rename:
- `GGUFProvider` → `OllamaProvider`
- File `gguf.go` → `ollama.go`
- Constructor `NewGGUFProvider()` → `NewOllamaProvider(url, model string)`

`OllamaProvider` reads connection parameters from `config.Cfg` rather than package-level constants.

### 5. Startup Flow

```
lmd <any command>
  → PersistentPreRunE:
    1. config.Load()          — load or generate config
    2. dao.Init(dbPath)       — open SQLite
  → Command-specific init (only for vsearch/query/embed):
    1. Check Ollama availability (GET config.Cfg.Embedding.Ollama.URL/api/tags)
    2. If unavailable: print clear error "Ollama not running at <url>. Start Ollama or edit ~/.config/lmd/config.yaml"
    3. If model missing: auto-pull via Ollama
```

### 6. Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | **New** — config loading, saving, defaults |
| `internal/embedding/gguf.go` | **Delete** — replaced by `ollama.go` |
| `internal/embedding/ollama.go` | **New** — Ollama-only provider, ~150 lines (down from ~550) |
| `internal/embedding/provider.go` | Unchanged |
| `internal/embedding/mock.go` | Unchanged |
| `internal/cli/root.go` | Add `config.Load()` call |
| `internal/cli/embed.go` | Read params from `config.Cfg` |
| `internal/cli/search.go` | Read params from `config.Cfg` |
| `internal/service/embedder.go` | Read batch size / truncation from `config.Cfg` |
| `go.mod` | Add `gopkg.in/yaml.v3` dependency |

### 7. Backward Compatibility

- Existing `--index` flag works as before
- No config file → auto-generated on first run, no user action needed
- Ollama at default URL with default model → zero-config experience

### 8. What Does NOT Change

- `EmbeddingProvider` interface — unchanged
- `MockProvider` — unchanged (tests)
- SQLite schema — unchanged
- Search/index logic — unchanged
- CLI command structure — unchanged

### 9. Testing

1. **Unit test**: `config.Load()` with missing file → generates default
2. **Unit test**: `config.Load()` with existing file → reads correctly
3. **Unit test**: `OllamaProvider` with mock HTTP server
4. **Integration test**: `lmd embed` with Ollama unavailable → clear error message
