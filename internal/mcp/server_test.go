package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandleInitialize(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("1"),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}

	resp := HandleRequest(req)
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
	if result.ProtocolVersion != "2024-11-05" {
		t.Fatalf("expected protocol version '2024-11-05', got %s", result.ProtocolVersion)
	}
}

func TestHandleToolsList(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("2"),
		Method:  "tools/list",
	}

	resp := HandleRequest(req)
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
	for _, name := range []string{"search", "search_lex", "search_vector", "get", "status", "list_collections", "memory_add", "memory_search"} {
		if !names[name] {
			t.Fatalf("expected '%s' tool", name)
		}
	}
}

func TestHandleToolsCall(t *testing.T) {
	called := false
	RegisterHandler(func(method string, params json.RawMessage) (interface{}, error) {
		called = true
		return map[string]string{"result": "ok"}, nil
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("3"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search","arguments":{"query":"test"}}`),
	}

	resp := HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("tools/call failed: %v", resp.Error.Message)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestHandleToolsCallError(t *testing.T) {
	RegisterHandler(func(method string, params json.RawMessage) (interface{}, error) {
		return nil, json.Unmarshal([]byte("invalid"), &struct{}{})
	})

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("4"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search"}`),
	}

	resp := HandleRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error from handler")
	}
	if resp.Error.Code != -32603 {
		t.Fatalf("expected code -32603, got %d", resp.Error.Code)
	}
}

func TestHandleToolsCallNoHandler(t *testing.T) {
	toolHandler = nil

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("5"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search"}`),
	}

	resp := HandleRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error with no handler")
	}
	if resp.Error.Code != -32603 {
		t.Fatalf("expected code -32603, got %d", resp.Error.Code)
	}
}

func TestHandleInitializedNotification(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	resp := HandleRequest(req)
	if resp.JSONRPC != "" {
		t.Fatal("expected empty response for notification")
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.Number("99"),
		Method:  "nonexistent",
	}
	resp := HandleRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestParseLine(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg, err := ParseLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "initialize" {
		t.Fatal("expected method 'initialize'")
	}
}

func TestParseLineEmpty(t *testing.T) {
	msg, err := ParseLine([]byte(""))
	if err != nil || msg != nil {
		t.Fatal("expected nil for empty line")
	}
}

func TestParseLineWhitespace(t *testing.T) {
	msg, err := ParseLine([]byte("   "))
	if err != nil || msg != nil {
		t.Fatal("expected nil for whitespace-only line")
	}
}

func TestParseLineInvalidJSON(t *testing.T) {
	_, err := ParseLine([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestServe(t *testing.T) {
	RegisterHandler(func(method string, params json.RawMessage) (interface{}, error) {
		return map[string]string{"ok": "true"}, nil
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"nonexistent"}`,
		`not json`,
		``,
	}, "\n")

	var buf bytes.Buffer
	Serve(strings.NewReader(input), &buf)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 4 {
		t.Fatalf("expected 4 response lines, got %d", len(lines))
	}

	var resp JSONRPCResponse
	json.Unmarshal(lines[0], &resp)
	if resp.Result == nil {
		t.Fatal("expected result for initialize")
	}

	json.Unmarshal(lines[3], &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatal("expected -32601 for unknown method")
	}
}

func TestServeSkipsNotifications(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	var buf bytes.Buffer
	Serve(strings.NewReader(input), &buf)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for notification, got %q", buf.String())
	}
}
