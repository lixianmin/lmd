package mcp

import (
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

	result, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatal("expected InitializeResult")
	}
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

	result, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatal("expected ToolsListResult")
	}
	if len(result.Tools) == 0 {
		t.Fatal("expected some tools")
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"search", "search_lex", "search_vector", "get", "status", "list_collections"} {
		if !names[name] {
			t.Fatalf("expected '%s' tool", name)
		}
	}
}

func TestParseLine(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg, err := parseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "initialize" {
		t.Fatal("expected method 'initialize'")
	}
}

func TestParseLineEmpty(t *testing.T) {
	msg, err := parseLine([]byte(""))
	if err != nil || msg != nil {
		t.Fatal("expected nil for empty line")
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("99"),
		Method:  "nonexistent",
	}
	resp := handleRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}
