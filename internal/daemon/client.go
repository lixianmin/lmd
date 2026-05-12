package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	httpClientTimeout  = 120 * time.Second      // HTTP 客户端总超时
	daemonStartTimeout = 30 * time.Second       // 等待 daemon 启动的最长等待时间
	daemonPollInterval = 500 * time.Millisecond // 轮询 daemon 是否存活的间隔
)

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://localhost:%d", port),
		client:  &http.Client{Timeout: httpClientTimeout},
	}
}

func (my *Client) IsAlive() bool {
	resp, err := my.client.Get(my.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (my *Client) EnsureDaemon() error {
	if my.IsAlive() {
		return nil
	}

	if IsRunning() {
		fmt.Fprintf(os.Stderr, "  Daemon already starting, waiting...\n")
	} else {
		if err := StartBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  Waiting for daemon to start\n")
	}

	deadline := time.Now().Add(daemonStartTimeout)
	for {
		if my.IsAlive() {
			fmt.Fprintf(os.Stderr, "  Daemon ready.\n")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon did not start within %s", daemonStartTimeout)
		}
		time.Sleep(daemonPollInterval)
	}
}

func (my *Client) Post(path string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := my.client.Post(my.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon returned %d: %s", resp.StatusCode, string(respBody))
	}
	return io.ReadAll(resp.Body)
}

func (my *Client) Get(path string) ([]byte, error) {
	resp, err := my.client.Get(my.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("daemon returned %d: %s", resp.StatusCode, string(respBody))
	}
	return io.ReadAll(resp.Body)
}

func (my *Client) Search(query, collection string, limit int, minScore float64, format string, jsonOutput bool) ([]byte, error) {
	return my.Post("/search", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
		"format":     format,
		"json":       jsonOutput,
	})
}

func (my *Client) VSearch(query, collection string, limit int, minScore float64) ([]byte, error) {
	return my.Post("/vsearch", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (my *Client) Hybrid(query, collection string, limit int, minScore float64) ([]byte, error) {
	return my.Post("/hybrid", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (my *Client) HyDE(query, collection string, limit int, minScore float64) ([]byte, error) {
	return my.Post("/hyde", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (my *Client) GetDoc(pathOrDocId string, full bool, from, lines int) ([]byte, error) {
	return my.Post("/get", map[string]interface{}{
		"path":  pathOrDocId,
		"full":  full,
		"from":  from,
		"lines": lines,
	})
}

func (my *Client) Status() ([]byte, error) {
	return my.Get("/status")
}

func (my *Client) CollectionAdd(path, name, mask string) ([]byte, error) {
	return my.Post("/collection/add", map[string]interface{}{
		"path": path,
		"name": name,
		"mask": mask,
	})
}

func (my *Client) CollectionRemove(name string) ([]byte, error) {
	return my.Post("/collection/remove", map[string]interface{}{
		"name": name,
	})
}

func (my *Client) CollectionList() ([]byte, error) {
	return my.Get("/collection/list")
}

func (my *Client) CollectionRename(oldName, newName string) ([]byte, error) {
	return my.Post("/collection/rename", map[string]interface{}{
		"old": oldName,
		"new": newName,
	})
}

func (my *Client) Rebuild() ([]byte, error) {
	return my.Post("/rebuild", nil)
}


