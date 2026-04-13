# Phase 4 - MCP Server + Context System Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan.

**Goal:** Add MCP (Model Context Protocol) server for agent integration, context system for path-level metadata, and polish the public API.

**Architecture:** MCP server exposes search/get/status tools via stdio protocol. Context system lets users attach descriptions to collection paths. Search results include matching context metadata.

**Tech Stack:** MCP stdio protocol (JSON-RPC over stdin/stdout), no new external dependencies.

**Spec:** `docs/superpowers/specs/2026-04-12-lmd-design.md`

**Depends on:** Phase 1 + 2 + 3 (complete)

---

## Chunk 1: Context System

### Task 1: Context store operations + CLI commands

**Files:**
- Create: `internal/store/context.go`
- Test: `internal/store/context_test.go`
- Create: `internal/cli/context.go`

- [ ] **Step 1: Write failing tests**

Create `internal/store/context_test.go`:
```go
package store

import (
	"testing"
)

func TestAddContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := AddContext(db, "notes", "work", "Work-related notes")
	if err != nil {
		t.Fatalf("AddContext failed: %v", err)
	}
}

func TestAddContextUpdate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "work", "v1")
	err := AddContext(db, "notes", "work", "v2")
	if err != nil {
		t.Fatalf("AddContext update failed: %v", err)
	}

	ctx, err := GetContext(db, "notes", "work")
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	if ctx != "v2" {
		t.Fatalf("expected v2, got %s", ctx)
	}
}

func TestGetContextNotFound(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_, err := GetContext(db, "notes", "nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent context")
	}
}

func TestRemoveContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "work", "desc")
	err := RemoveContext(db, "notes", "work")
	if err != nil {
		t.Fatalf("RemoveContext failed: %v", err)
	}

	_, err = GetContext(db, "notes", "work")
	if err == nil {
		t.Fatal("expected error after removal")
	}
}

func TestListContexts(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "", "global notes context")
	_ = AddContext(db, "notes", "work", "work notes")
	_ = AddContext(db, "docs", "api", "API docs")

	contexts, err := ListContexts(db, "notes")
	if err != nil {
		t.Fatalf("ListContexts failed: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts for notes, got %d", len(contexts))
	}
}

func TestFindBestContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "", "collection-level")
	_ = AddContext(db, "notes", "work", "work-level")

	ctx := FindBestContext(db, "notes", "work/project-a.md")
	if ctx != "work-level" {
		t.Fatalf("expected work-level, got %q", ctx)
	}

	ctx2 := FindBestContext(db, "notes", "personal/diary.md")
	if ctx2 != "collection-level" {
		t.Fatalf("expected collection-level, got %q", ctx2)
	}
}
```

- [ ] **Step 2: Implement context.go**

Create `internal/store/context.go`:
```go
package store

import (
	"database/sql"
	"errors"
	"path"
	"strings"
)

type ContextRecord struct {
	Collection string
	Path       string
	Context    string
}

func AddContext(db *sql.DB, collection, p, context string) error {
	stmt, err := db.Prepare(
		"INSERT OR REPLACE INTO path_contexts (collection, path, context) VALUES (?, ?, ?)",
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(collection, p, context)
	return err
}

func GetContext(db *sql.DB, collection, p string) (string, error) {
	stmt, err := db.Prepare("SELECT context FROM path_contexts WHERE collection=? AND path=?")
	if err != nil {
		return "", err
	}
	defer stmt.Close()

	var ctx string
	err = stmt.QueryRow(collection, p).Scan(&ctx)
	if err == sql.ErrNoRows {
		return "", errors.New("context not found")
	}
	return ctx, err
}

func RemoveContext(db *sql.DB, collection, p string) error {
	stmt, err := db.Prepare("DELETE FROM path_contexts WHERE collection=? AND path=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(collection, p)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("context not found")
	}
	return nil
}

func ListContexts(db *sql.DB, collection string) ([]ContextRecord, error) {
	stmt, err := db.Prepare(
		"SELECT collection, path, context FROM path_contexts WHERE collection=? ORDER BY path",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contexts []ContextRecord
	for rows.Next() {
		var c ContextRecord
		if err := rows.Scan(&c.Collection, &c.Path, &c.Context); err != nil {
			return nil, err
		}
		contexts = append(contexts, c)
	}
	return contexts, rows.Err()
}

func FindBestContext(db *sql.DB, collection, docPath string) string {
	parts := strings.Split(docPath, "/")
	for i := len(parts); i >= 0; i-- {
		p := strings.Join(parts[:i], "/")
		ctx, err := GetContext(db, collection, p)
		if err == nil && ctx != "" {
			return ctx
		}
	}
	return ""
}
```

- [ ] **Step 3: Run tests**

Run: `go test -tags "fts5" ./internal/store/ -run TestContext -v`

- [ ] **Step 4: Create context CLI commands**

Create `internal/cli/context.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage path context metadata",
}

var contextAddCmd = &cobra.Command{
	Use:   "add <collection/path> <description>",
	Short: "Add or update context for a path",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		collection, p := parseContextPath(args[0])
		return store.AddContext(db, collection, p, args[1])
	},
}

var contextRemoveCmd = &cobra.Command{
	Use:   "remove <collection/path>",
	Short: "Remove context for a path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		collection, p := parseContextPath(args[0])
		return store.RemoveContext(db, collection, p)
	},
}

var contextListCmd = &cobra.Command{
	Use:   "list <collection>",
	Short: "List contexts for a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		contexts, err := store.ListContexts(db, args[0])
		if err != nil {
			return err
		}
		if len(contexts) == 0 {
			fmt.Println("No contexts found.")
			return nil
		}
		for _, c := range contexts {
			if c.Path == "" {
				fmt.Printf("  lmd://%s (collection-level)\n", c.Collection)
			} else {
				fmt.Printf("  lmd://%s/%s\n", c.Collection, c.Path)
			}
			fmt.Printf("    %s\n", c.Context)
		}
		return nil
	},
}

func parseContextPath(uri string) (collection, p string) {
	uri = strings.TrimPrefix(uri, "lmd://")
	parts := strings.SplitN(uri, "/", 2)
	collection = parts[0]
	if len(parts) > 1 {
		p = parts[1]
	}
	return
}

func init() {
	contextCmd.AddCommand(contextAddCmd)
	contextCmd.AddCommand(contextRemoveCmd)
	contextCmd.AddCommand(contextListCmd)
	rootCmd.AddCommand(contextCmd)
}
```

- [ ] **Step 5: Build and test**

Run: `make build`

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add context system (store + CLI)"
```

---

## Chunk 2: MCP Server

### Task 2: MCP stdio protocol server

The MCP protocol is JSON-RPC over stdio. We implement a minimal server that handles `initialize`, `tools/list`, and `tools/call` methods.

**Files:**
- Create: `internal/mcp/protocol.go` (JSON-RPC types)
- Create: `internal/mcp/server.go` (server loop + dispatch)
- Create: `internal/mcp/handler.go` (tool handlers)
- Test: `internal/mcp/server_test.go`
- Create: `internal/cli/mcp.go` (CLI command)

- [ ] **Step 1: Write failing test**

Create `internal/mcp/server_test.go`:
```go
package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestHandleInitialize(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("1"),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}

	resp := handleRequest(req)
	if resp.Error != nil {
		t.Fatalf("initialize failed: %v", resp.Error.Message)
	}

	var result InitializeResult
	json.Unmarshal(resp.Result, &result)
	if result.ServerInfo.Name != "lmd" {
		t.Fatalf("expected server name 'lmd', got %s", result.ServerInfo.Name)
	}
}

func TestHandleToolsList(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("2"),
		Method:  "tools/list",
	}

	resp := handleRequest(req)
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %v", resp.Error.Message)
	}

	var result ToolsListResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Tools) == 0 {
		t.Fatal("expected some tools")
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	if !names["search"] {
		t.Fatal("expected 'search' tool")
	}
	if !names["get"] {
		t.Fatal("expected 'get' tool")
	}
}

func TestParseMessage(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg, err := parseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "initialize" {
		t.Fatal("expected method 'initialize'")
	}
}
```

- [ ] **Step 2: Implement MCP protocol types**

Create `internal/mcp/protocol.go`:
```go
package mcp

import (
	"encoding/json"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolsListResult struct {
	Tools []ToolDef `json:"tools"`
}
```

- [ ] **Step 3: Implement server + handler**

Create `internal/mcp/server.go`:
```go
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
)

type ToolHandler func(method string, params json.RawMessage) (interface{}, error)

var toolDefs = []ToolDef{
	{Name: "search", Description: "Hybrid search (BM25 + vector)"},
	{Name: "search_lex", Description: "BM25 keyword search"},
	{Name: "search_vector", Description: "Vector semantic search"},
	{Name: "get", Description: "Retrieve document by path or docid"},
	{Name: "status", Description: "Index status"},
	{Name: "list_collections", Description: "List all collections"},
}

var toolHandler ToolHandler

func RegisterHandler(h ToolHandler) {
	toolHandler = h
}

func parseLine(line []byte) (*JSONRPCRequest, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, nil
	}
	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func handleRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		result := InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      ServerInfo{Name: "lmd", Version: "0.1.0"},
		}
		data, _ := json.Marshal(result)
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}

	case "notifications/initialized":
		return JSONRPCResponse{}

	case "tools/list":
		data, _ := json.Marshal(ToolsListResult{Tools: toolDefs})
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}

	case "tools/call":
		if toolHandler == nil {
			return JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32603, Message: "no tool handler registered"},
			}
		}
		var params struct {
			Name   string          `json:"name"`
			Params json.RawMessage `json:"arguments"`
		}
		json.Unmarshal(req.Params, &params)
		result, err := toolHandler(params.Name, params.Params)
		if err != nil {
			return JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32603, Message: err.Error()},
			}
		}
		data, _ := json.Marshal(result)
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}

	default:
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JSONRPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func Serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		req, err := parseLine(scanner.Bytes())
		if err != nil || req == nil {
			continue
		}
		resp := handleRequest(*req)
		if resp.ID.String() != "" {
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			if _, err := w.Write(data); err != nil {
				log.Printf("mcp write error: %v", err)
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -v`

- [ ] **Step 5: Create MCP CLI command**

Create `internal/cli/mcp.go`:
```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio)",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return err
		}

		searcher := service.NewSearcher(db, tok)
		provider := embedding.NewMockProvider(1024)

		mcp.RegisterHandler(func(name string, params json.RawMessage) (interface{}, error) {
			return handleToolCall(db, searcher, provider, name, params)
		})

		mcp.Serve(os.Stdin, os.Stdout)
		return nil
	},
}

func handleToolCall(db *sql.DB, searcher *service.Searcher, provider embedding.EmbeddingProvider, name string, params json.RawMessage) (interface{}, error) {
	switch name {
	case "search":
		var p struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection,omitempty"`
			Limit      int     `json:"limit,omitempty"`
			MinScore   float64 `json:"min_score,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchHybrid(provider, p.Query, p.Collection, p.Limit, p.MinScore)

	case "search_lex":
		var p struct {
			Query      string `json:"query"`
			Collection string `json:"collection,omitempty"`
			Limit      int    `json:"limit,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchLex(p.Query, p.Collection, p.Limit, 0)

	case "search_vector":
		var p struct {
			Query      string `json:"query"`
			Collection string `json:"collection,omitempty"`
			Limit      int    `json:"limit,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchVector(provider, p.Query, p.Collection, p.Limit, 0)

	case "get":
		var p struct {
			PathOrDocID string `json:"path_or_docid"`
		}
		json.Unmarshal(params, &p)
		return getDocument(db, p.PathOrDocID)

	case "status":
		return getStatus(db)

	case "list_collections":
		cols, err := store.ListCollections(db)
		if err != nil {
			return nil, err
		}
		return cols, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func getDocument(db *sql.DB, pathOrDocID string) (interface{}, error) {
	if len(pathOrDocID) > 0 && pathOrDocID[0] == '#' {
		return store.GetDocumentByDocID(db, pathOrDocID[1:])
	}
	parts := splitPath(pathOrDocID)
	if len(parts) == 2 {
		return store.GetDocumentByPath(db, parts[0], parts[1])
	}
	return nil, fmt.Errorf("invalid path format, use collection/path or #docid")
}

func getStatus(db *sql.DB) (interface{}, error) {
	return store.ListCollections(db)
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
```

Note: need to add `"strings"` import to `internal/cli/context.go` and add missing `store` import.

- [ ] **Step 6: Build and test**

Run: `make test && make build`

- [ ] **Step 7: E2E test**

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./lmd --index /tmp/lmd-phase3/test.sqlite mcp
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./lmd --index /tmp/lmd-phase3/test.sqlite mcp
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status"}}' | ./lmd --index /tmp/lmd-phase3/test.sqlite mcp
echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"query":"并发编程"}}}' | ./lmd --index /tmp/lmd-phase3/test.sqlite mcp
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: add MCP server + context CLI commands"
```

---

## Summary

Phase 4 adds:
- Context system: `context add/remove/list` CLI + `FindBestContext` store function
- MCP server: stdio JSON-RPC with 6 tools (search, search_lex, search_vector, get, status, list_collections)
- `mcp` CLI command to start the server

Deferred (requires GGUF model loading):
- Reranker integration
- Query expansion
- HTTP transport for MCP
