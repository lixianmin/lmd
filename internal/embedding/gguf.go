package embedding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lixianmin/got/loom"
	"github.com/lixianmin/logo"
)

const defaultOllamaModel = "qwen3-embedding:0.6b-q8_0"
const defaultOllamaURL = "http://localhost:11434"

const defaultModelFilename = "Qwen3-Embedding-0.6B-Q8_0.gguf"

const defaultModelHuggingFacePath = "Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/" + defaultModelFilename

var modelMirrorHosts = []string{
	"https://hf-mirror.com",
	"https://huggingface.co",
}

const serverIdleTimeout = 5 * time.Minute

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd")
}

func pidFilePath() string {
	return filepath.Join(cacheDir(), "llama-server.pid")
}

func lastActiveFilePath() string {
	return filepath.Join(cacheDir(), "llama-server.last-active")
}

func writePid(pid int) error {
	return os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

func readPid() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func touchLastActive() {
	os.WriteFile(lastActiveFilePath(), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(os.Signal(nil))
	return err == nil
}

func killServerByPidFile() {
	pid, err := readPid()
	if err != nil {
		return
	}
	if !isProcessAlive(pid) {
		cleanupServerFiles()
		return
	}
	proc, _ := os.FindProcess(pid)
	proc.Signal(os.Interrupt)
	done := make(chan error, 1)
	loom.Go(func(later loom.Later) {
		_, _ = proc.Wait()
		done <- nil
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		proc.Kill()
	}
	cleanupServerFiles()
	logo.Info("stopped idle llama-server (pid %d)", pid)
}

func cleanupServerFiles() {
	os.Remove(pidFilePath())
	os.Remove(lastActiveFilePath())
}

func startWatchdog(serverPid int) {
	pidFile := pidFilePath()
	activeFile := lastActiveFilePath()
	timeoutSec := int(serverIdleTimeout.Seconds())
	checkSec := 30

	script := fmt.Sprintf(`while kill -0 %d 2>/dev/null; do
  if [ -f %s ]; then
    last=$(cat %s)
    now=$(date +%%s)
    elapsed=$((now - last))
    if [ $elapsed -gt %d ]; then
      kill %d
      rm -f %s %s
      exit 0
    fi
  fi
  sleep %d
done
rm -f %s %s`, serverPid, activeFile, activeFile, timeoutSec, serverPid, pidFile, activeFile, checkSec, pidFile, activeFile)

	cmd := exec.Command("bash", "-c", script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
	logo.Info("watchdog started: will kill llama-server after %v idle", serverIdleTimeout)
}

type GGUFProvider struct {
	modelPath string
	dim       int
	baseURL   string
	mu        sync.Mutex
	started   bool
	useOllama bool
	ollamaURL string
	ollamaMod string
}

func NewGGUFProvider(modelPath string) *GGUFProvider {
	return &GGUFProvider{
		modelPath: modelPath,
		dim:       1024,
		baseURL:   "http://127.0.0.1:61999",
	}
}

func ollamaAvailable() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(defaultOllamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func ollamaModelExists(model string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(defaultOllamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	for _, m := range result.Models {
		if m.Name == model || strings.HasPrefix(m.Name, model+":") {
			return true
		}
	}
	return false
}

func ollamaPull(model string) error {
	fmt.Printf("Pulling embedding model via Ollama: %s (~639MB)...\n", model)
	body, _ := json.Marshal(map[string]string{"name": model, "stream": "false"})
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Post(defaultOllamaURL+"/api/pull", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama pull failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama pull returned %d: %s", resp.StatusCode, string(b))
	}
	fmt.Printf("Model %s pulled successfully.\n", model)
	return nil
}

func (my *GGUFProvider) Init() error {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.started {
		return nil
	}

	if ollamaAvailable() {
		if !ollamaModelExists(defaultOllamaModel) {
			if err := ollamaPull(defaultOllamaModel); err != nil {
				logo.Warn("ollama pull failed: %s, falling back to llama-server", err)
			} else {
				my.useOllama = true
				my.ollamaURL = defaultOllamaURL
				my.ollamaMod = defaultOllamaModel
				my.started = true
				logo.Info("embedding: Ollama %s (warming up)", my.ollamaMod)
				my.warmup()
				return nil
			}
		} else {
			my.useOllama = true
			my.ollamaURL = defaultOllamaURL
			my.ollamaMod = defaultOllamaModel
			my.started = true
			logo.Info("embedding: Ollama %s (warming up)", my.ollamaMod)
			my.warmup()
			return nil
		}
	}

	logo.Info("embedding: llama-server (fallback)")
	return my.startLLamaServer()
}

func (my *GGUFProvider) callEmbedAPI(input interface{}) ([][]float32, error) {
	if !my.started {
		if err := my.Init(); err != nil {
			return nil, err
		}
	}

	if my.useOllama {
		return my.callOllamaEmbed(input)
	}
	return my.callLlamaEmbed(input)
}

func (my *GGUFProvider) startLLamaServer() error {
	pid, pidErr := readPid()
	if pidErr == nil && isProcessAlive(pid) {
		data, err := os.ReadFile(lastActiveFilePath())
		if err == nil {
			ts, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if time.Since(time.Unix(ts, 0)) > serverIdleTimeout {
				logo.Info("llama-server idle for %v, restarting", serverIdleTimeout)
				killServerByPidFile()
			} else {
				resp, err := http.Get(my.baseURL + "/health")
				if err == nil && resp.StatusCode == http.StatusOK {
					resp.Body.Close()
					my.started = true
					touchLastActive()
					logo.Info("reusing llama-server on :61999 (pid %d)", pid)
					return nil
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		}
	} else if pidErr == nil {
		cleanupServerFiles()
	}

	logo.Info("starting llama-server with model: %s", filepath.Base(my.modelPath))
	cmd := exec.Command("llama-server",
		"-m", my.modelPath,
		"--pooling", "mean",
		"-ngl", "99",
		"-t", "4",
		"--port", "61999",
		"--host", "127.0.0.1",
		"--embedding",
		"--log-disable",
		"-b", "4096",
		"--ubatch-size", "4096",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		logo.Error("failed to start llama-server: %s", err)
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	writePid(cmd.Process.Pid)

	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get(my.baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			my.started = true
			touchLastActive()
			startWatchdog(cmd.Process.Pid)
			logo.Info("llama-server ready on :61999 (pid %d)", cmd.Process.Pid)
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	logo.Error("llama-server failed to start within 30s")
	return fmt.Errorf("llama-server failed to start within 30s")
}

func DefaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "lmd", "models", defaultModelFilename)
}

func ModelExists() bool {
	_, err := os.Stat(DefaultModelPath())
	return err == nil
}

func EnsureModel() error {
	if ollamaAvailable() {
		if !ollamaModelExists(defaultOllamaModel) {
			if err := ollamaPull(defaultOllamaModel); err != nil {
				return fmt.Errorf("ollama pull failed: %w", err)
			}
		}
		return nil
	}

	if ModelExists() {
		return nil
	}

	modelPath := DefaultModelPath()
	fmt.Printf("Ollama not found. Embedding model not found: %s\n", modelPath)
	fmt.Printf("Need to download %s (~610MB). Download? [Y/n] ", defaultModelFilename)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	answer := strings.TrimSpace(strings.ToLower(input))
	if answer != "" && answer != "y" && answer != "yes" {
		return fmt.Errorf("model download cancelled")
	}

	return downloadModel(modelPath)
}

func IsOllamaMode() bool {
	return ollamaAvailable()
}

func downloadModel(modelPath string) error {
	if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	var lastErr error
	for i, host := range modelMirrorHosts {
		url := host + "/" + defaultModelHuggingFacePath
		mirror := "HuggingFace"
		if i == 0 {
			mirror = "hf-mirror (China)"
		}
		fmt.Printf("Downloading from %s ...\n", mirror)
		lastErr = downloadFile(url, modelPath)
		if lastErr == nil {
			fmt.Printf("Download complete: %s\n", modelPath)
			return nil
		}
		fmt.Printf("Download from %s failed: %v\n", mirror, lastErr)
	}
	return fmt.Errorf("all download mirrors failed: %w", lastErr)
}

func downloadFile(url, dest string) error {
	tmpPath := dest + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(tmpPath)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	written := int64(0)
	buf := make([]byte, 32*1024)
	lastPrint := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			written += int64(n)
			if total > 0 && time.Since(lastPrint) > 3*time.Second {
				pct := float64(written) / float64(total) * 100
				fmt.Printf("  %.0f%% (%d/%d MB)\n", pct, written/1024/1024, total/1024/1024)
				lastPrint = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dest)
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type embeddingRequest struct {
	Input interface{} `json:"input"`
	Model string      `json:"model"`
}

type embeddingItem struct {
	Index     int         `json:"index"`
	Embedding [][]float32 `json:"embedding"`
}

type llamaEmbedResponse []embeddingItem

func (my *GGUFProvider) warmup() {
	body, _ := json.Marshal(map[string]interface{}{"model": my.ollamaMod, "input": "warmup"})
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(my.ollamaURL+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		logo.Warn("ollama warmup failed: %s", err)
		return
	}
	resp.Body.Close()

	keepBody, _ := json.Marshal(map[string]interface{}{"model": my.ollamaMod, "keep_alive": "30m"})
	keepResp, err := client.Post(my.ollamaURL+"/api/generate", "application/json", bytes.NewReader(keepBody))
	if err == nil {
		keepResp.Body.Close()
	}
	logo.Info("embedding: Ollama %s (ready)", my.ollamaMod)
}

var ollamaHTTPClient = &http.Client{Timeout: 120 * time.Second}

func (my *GGUFProvider) callOllamaEmbed(input interface{}) ([][]float32, error) {
	body, _ := json.Marshal(map[string]interface{}{"model": my.ollamaMod, "input": input})
	resp, err := ollamaHTTPClient.Post(my.ollamaURL+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed API returned %d: %s", resp.StatusCode, string(b))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}
	return result.Embeddings, nil
}

func (my *GGUFProvider) callLlamaEmbed(input interface{}) ([][]float32, error) {
	body, _ := json.Marshal(embeddingRequest{Input: input, Model: "default"})
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(my.baseURL+"/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		logo.Error("embedding API returned %d: %s", resp.StatusCode, string(b))
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(b))
	}

	var items llamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	touchLastActive()

	vecs := make([][]float32, len(items))
	for _, item := range items {
		if len(item.Embedding) > 0 {
			vecs[item.Index] = item.Embedding[0]
		}
	}
	return vecs, nil
}

func (my *GGUFProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := my.callEmbedAPI(text)
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (my *GGUFProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return my.callEmbedAPI(texts)
}

func (my *GGUFProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, query)
}

func (my *GGUFProvider) Dimension() int { return my.dim }
func (my *GGUFProvider) ModelName() string {
	if my.useOllama {
		return my.ollamaMod
	}
	return "Qwen3-Embedding-0.6B-Q8_0"
}
func (my *GGUFProvider) Close() error {
	return nil
}
