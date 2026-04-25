package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"

	"github.com/lixianmin/logo"
)

const mcpScannerBufSize = 1024 * 1024 // MCP JSON-RPC 扫描器缓冲区大小（1 MB）

var toolDefs = []ToolDef{
	{Name: "search", Description: "Hybrid search (BM25 + vector)"},
	{Name: "search_lex", Description: "BM25 keyword search"},
	{Name: "search_vector", Description: "Vector semantic search"},
	{Name: "get", Description: "Retrieve document by path or docid"},
	{Name: "status", Description: "Index status"},
	{Name: "list_collections", Description: "List all collections"},
	{Name: "memory_add", Description: "Add agent memory"},
	{Name: "memory_query", Description: "Query agent memories with hybrid search"},
	{Name: "memory_delete", Description: "Delete an agent memory by id"},
}

type ToolHandler func(method string, params json.RawMessage) (interface{}, error)

var (
	toolHandlerMu sync.RWMutex
	toolHandler   ToolHandler
)

func RegisterHandler(h ToolHandler) {
	toolHandlerMu.Lock()
	toolHandler = h
	toolHandlerMu.Unlock()
}

func getToolHandler() ToolHandler {
	toolHandlerMu.RLock()
	h := toolHandler
	toolHandlerMu.RUnlock()
	return h
}

func ParseLine(line []byte) (*JSONRPCRequest, error) {
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

func HandleRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: InitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities:    map[string]any{"tools": map[string]any{}},
				ServerInfo:      ServerInfo{Name: "lmd", Version: "0.1.0"},
			},
		}

	case "notifications/initialized":
		return JSONRPCResponse{}

	case "tools/list":
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: ToolsListResult{Tools: toolDefs},
		}

	case "tools/call":
		h := getToolHandler()
		if h == nil {
			return JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32603, Message: "no tool handler registered"},
			}
		}
		var params struct {
			Name   string          `json:"name"`
			Params json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32602, Message: "invalid params: " + err.Error()},
			}
		}
		result, err := h(params.Name, params.Params)
		if err != nil {
			return JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &JSONRPCError{Code: -32603, Message: err.Error()},
			}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	default:
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JSONRPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func Serve(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, mcpScannerBufSize), mcpScannerBufSize)
	for scanner.Scan() {
		req, err := ParseLine(scanner.Bytes())
		if err != nil || req == nil {
			continue
		}
		resp := HandleRequest(*req)
		if resp.ID.String() != "" {
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			if _, err := w.Write(data); err != nil {
				logo.Warn("mcp write error: %s", err)
				return
			}
		}
	}
}
