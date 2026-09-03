package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ToolDiscoverer is an optional capability for downstream MCP clients that support tools/list.
type ToolDiscoverer interface {
	ListTools(ctx context.Context) ([]ToolDefinition, error)
}

// ListTools discovers the tools exposed by the downstream MCP server.
func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	requestBody, err := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  ToolsListMethod,
	})
	if err != nil {
		return nil, fmt.Errorf("encode tools/list request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create tools/list request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover downstream tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("downstream MCP server returned HTTP %d", resp.StatusCode)
	}

	var rpcResponse JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResponse); err != nil {
		return nil, fmt.Errorf("decode tools/list response: %w", err)
	}
	if rpcResponse.Error != nil {
		return nil, fmt.Errorf("downstream MCP error %d: %s", rpcResponse.Error.Code, rpcResponse.Error.Message)
	}

	encoded, err := json.Marshal(rpcResponse.Result)
	if err != nil {
		return nil, fmt.Errorf("encode tools/list result: %w", err)
	}
	var result ToolsListResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode tools/list result: %w", err)
	}
	return result.Tools, nil
}
