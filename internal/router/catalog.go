package router

import (
	"context"
	"fmt"
	"sort"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
)

// Catalog provides the gateway's public tool view by combining local tools
// with tools discovered from downstream MCP servers.
type Catalog struct {
	local     *tools.Registry
	discovery *Discovery
}

func NewCatalog(local *tools.Registry, discovery *Discovery) *Catalog {
	return &Catalog{local: local, discovery: discovery}
}

// List returns a deterministic, collision-safe view of all gateway tools.
func (c *Catalog) List(ctx context.Context) ([]mcp.ToolDefinition, error) {
	if c.local == nil {
		return nil, fmt.Errorf("local tool registry is unavailable")
	}
	if c.discovery == nil {
		return nil, fmt.Errorf("tool discovery is unavailable")
	}

	result := make([]mcp.ToolDefinition, 0)
	seen := make(map[string]string)

	for _, tool := range c.local.List() {
		seen[tool.Name] = "local"
		result = append(result, mcp.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	remoteTools, err := c.discovery.Discover(ctx)
	if err != nil {
		return nil, err
	}
	for _, tool := range remoteTools {
		if previous, exists := seen[tool.Name]; exists {
			return nil, fmt.Errorf("tool %q is exposed by both %q and downstream servers", tool.Name, previous)
		}
		seen[tool.Name] = "downstream"
		result = append(result, tool)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
