package mcp

import "encoding/json"

// JSONRPCRequest is the transport-level envelope used by MCP.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is returned for requests that expect a response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a protocol-level failure.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	InvalidRequest  = -32600
	MethodNotFound  = -32601
	InvalidParams   = -32602
	InternalError   = -32603
)
