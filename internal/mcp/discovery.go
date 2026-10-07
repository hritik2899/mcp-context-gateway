package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

type ToolDiscoverer interface {
	ListTools(context.Context) ([]ToolDefinition, error)
}

func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	// Discovery is read-only and may restart once after an expired session.
	for attempt := 0; attempt < 2; attempt++ {
		result, err := c.listTools(ctx)
		if !errors.Is(err, ErrSessionExpired) {
			return result, err
		}
	}
	return nil, ErrSessionExpired
}
func (c *HTTPClient) listTools(ctx context.Context) ([]ToolDefinition, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	sid := c.currentSession()
	result := []ToolDefinition{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, _, err := c.exchange(ctx, ToolsListMethod, params, sid, false)
		if err != nil {
			if errors.Is(err, ErrSessionExpired) {
				c.invalidate(sid)
			}
			return nil, err
		}
		var response ToolsListResult
		if err := json.Unmarshal(raw, &response); err != nil || response.Tools == nil {
			return nil, errors.New("invalid downstream tools list")
		}
		result = append(result, response.Tools...)
		if len(result) > 10000 {
			return nil, errors.New("downstream tool count exceeds limit")
		}
		cursor = response.NextCursor
		if cursor == "" {
			return result, nil
		}
		if seen[cursor] {
			return nil, errors.New("downstream repeated pagination cursor")
		}
		seen[cursor] = true
	}
	return nil, errors.New("downstream pagination exceeds limit")
}
