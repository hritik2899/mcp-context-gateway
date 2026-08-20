package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client is the minimal downstream MCP client contract used by the gateway.
type Client interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error)
}

// HTTPClient communicates with a downstream MCP endpoint over HTTP.
type HTTPClient struct {
	endpoint string
	http     *http.Client
}

func NewHTTPClient(endpoint string) *HTTPClient {
	return &HTTPClient{endpoint: endpoint, http: http.DefaultClient}
}

func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error) {
	params, err := json.Marshal(CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallToolResult{}, fmt.Errorf("encode tool call: %w", err)
	}

	requestBody, err := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  ToolsCallMethod,
		Params:  params,
	})
	if err != nil {
		return CallToolResult{}, fmt.Errorf("encode JSON-RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return CallToolResult{}, fmt.Errorf("create downstream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("call downstream MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CallToolResult{}, fmt.Errorf("downstream MCP server returned HTTP %d", resp.StatusCode)
	}

	var rpcResponse JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResponse); err != nil {
		return CallToolResult{}, fmt.Errorf("decode downstream response: %w", err)
	}
	if rpcResponse.Error != nil {
		return CallToolResult{}, fmt.Errorf("downstream MCP error %d: %s", rpcResponse.Error.Code, rpcResponse.Error.Message)
	}

	result, ok := rpcResponse.Result.(map[string]any)
	if !ok {
		return CallToolResult{}, fmt.Errorf("downstream response has invalid result")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("encode downstream result: %w", err)
	}

	return CallToolResult{Content: []ContentBlock{{Type: "text", Text: string(encoded)}}}, nil
}
