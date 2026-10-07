package mcp

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the deliberately supported, non-batch MCP revision.
const ProtocolVersion = "2025-06-18"

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string { return e.Message }
func InvalidTool(name string) error {
	return &JSONRPCError{Code: InvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

// Raw is the original result. Remote results are forwarded losslessly, including
// extension fields; locally constructed results use the typed fields below.
type CallToolResult struct {
	Content           []ContentBlock  `json:"content"`
	IsError           bool            `json:"isError,omitempty"`
	StructuredContent map[string]any  `json:"structuredContent,omitempty"`
	Meta              map[string]any  `json:"_meta,omitempty"`
	Raw               json.RawMessage `json:"-"`
}

func (r *CallToolResult) UnmarshalJSON(data []byte) error {
	type plain CallToolResult
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Content == nil {
		return fmt.Errorf("tool result must contain a content array")
	}
	*r = CallToolResult(p)
	r.Raw = append(json.RawMessage(nil), data...)
	return nil
}
func (r CallToolResult) MarshalJSON() ([]byte, error) {
	if r.Raw != nil {
		return r.Raw, nil
	}
	type plain CallToolResult
	return json.Marshal(plain(r))
}

type ContentBlock struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type plain ContentBlock
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Type == "" {
		return fmt.Errorf("content block requires type")
	}
	*b = ContentBlock(p)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Raw != nil {
		return b.Raw, nil
	}
	if b.Type == "text" {
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: b.Type, Text: b.Text})
	}
	type plain ContentBlock
	return json.Marshal(plain(b))
}
func TextResult(text string, isError bool) CallToolResult {
	return CallToolResult{Content: []ContentBlock{{Type: "text", Text: text}}, IsError: isError}
}
func NormalizeResult(value any) (CallToolResult, error) {
	switch v := value.(type) {
	case CallToolResult:
		return v, nil
	case *CallToolResult:
		if v != nil {
			return *v, nil
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("encode tool output: %w", err)
	}
	return TextResult(string(data), false), nil
}

const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)
const (
	InitializeMethod        = "initialize"
	InitializedNotification = "notifications/initialized"
	ToolsCallMethod         = "tools/call"
)
