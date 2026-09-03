package router

import (
	"context"
	"fmt"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
)

// Discovery aggregates tool definitions from downstream MCP servers.
type Discovery struct {
	servers *ServerRegistry
}

func NewDiscovery(servers *ServerRegistry) *Discovery {
	return &Discovery{servers: servers}
}

// Discover returns the union of tools exposed by registered downstream servers.
// A duplicate tool name is rejected rather than silently choosing one backend.
func (d *Discovery) Discover(ctx context.Context) ([]mcp.ToolDefinition, error) {
	if d.servers == nil {
		return nil, fmt.Errorf("server registry is unavailable")
	}

	var result []mcp.ToolDefinition
	seen := make(map[string]string)

	for _, server := range d.servers.List() {
		discoverer, ok := server.Client.(mcp.ToolDiscoverer)
		if !ok {
			return nil, fmt.Errorf("server %q does not support tool discovery", server.Name)
		}

		tools, err := discoverer.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("discover tools from server %q: %w", server.Name, err)
		}
		for _, tool := range tools {
			if previous, exists := seen[tool.Name]; exists {
				return nil, fmt.Errorf("tool %q is exposed by both %q and %q", tool.Name, previous, server.Name)
			}
			seen[tool.Name] = server.Name
			result = append(result, tool)
		}
	}

	return result, nil
}
