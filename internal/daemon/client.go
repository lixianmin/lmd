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

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://localhost:%d", port),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) IsAlive() bool {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) EnsureDaemon() error {
	if c.IsAlive() {
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

	for {
		if c.IsAlive() {
			fmt.Fprintf(os.Stderr, "  Daemon ready.\n")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (c *Client) Post(path string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *Client) Get(path string) ([]byte, error) {
	resp, err := c.client.Get(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *Client) Search(query, collection string, limit int, minScore float64, format string, jsonOutput bool) ([]byte, error) {
	return c.Post("/search", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
		"format":     format,
		"json":       jsonOutput,
	})
}

func (c *Client) VSearch(query, collection string, limit int, minScore float64) ([]byte, error) {
	return c.Post("/vsearch", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (c *Client) Query(query, collection string, limit int, minScore float64) ([]byte, error) {
	return c.Post("/query", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (c *Client) HyDE(query, collection string, limit int, minScore float64) ([]byte, error) {
	return c.Post("/hyde", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}

func (c *Client) GetDoc(pathOrDocId string, full bool, from, lines int) ([]byte, error) {
	return c.Post("/get", map[string]interface{}{
		"path":  pathOrDocId,
		"full":  full,
		"from":  from,
		"lines": lines,
	})
}

func (c *Client) Status() ([]byte, error) {
	return c.Get("/status")
}

func (c *Client) CollectionAdd(path, name, mask string) ([]byte, error) {
	return c.Post("/collection/add", map[string]interface{}{
		"path": path,
		"name": name,
		"mask": mask,
	})
}

func (c *Client) CollectionRemove(name string) ([]byte, error) {
	return c.Post("/collection/remove", map[string]interface{}{
		"name": name,
	})
}

func (c *Client) CollectionList() ([]byte, error) {
	return c.Get("/collection/list")
}

func (c *Client) CollectionRename(oldName, newName string) ([]byte, error) {
	return c.Post("/collection/rename", map[string]interface{}{
		"old": oldName,
		"new": newName,
	})
}

func (c *Client) Rebuild() ([]byte, error) {
	return c.Post("/rebuild", nil)
}

func (c *Client) MemoryAdd(content, memType string) ([]byte, error) {
	return c.Post("/memory/add", map[string]interface{}{
		"content": content,
		"type":    memType,
	})
}

func (c *Client) MemorySearch(query string, limit int, memType string) ([]byte, error) {
	return c.Post("/memory/search", map[string]interface{}{
		"query": query,
		"limit": limit,
		"type":  memType,
	})
}
