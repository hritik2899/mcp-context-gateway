package router

import (
	"context"
	"testing"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
)

type discoveryClient struct {
	tools []mcp.ToolDefinition
}

func (c *discoveryClient) CallTool(ctx context.Context, name string, arguments map[string]any) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{}, nil
}

func (c *discoveryClient) ListTools(ctx context.Context) ([]mcp.ToolDefinition, error) {
	return c.tools, nil
}

func TestDiscoveryAggregatesRegisteredServers(t *testing.T) {
	servers := NewServerRegistry()
	if err := servers.Register(Server{Name: "github", Client: &discoveryClient{tools: []mcp.ToolDefinition{{Name: "github.search"}}}}); err != nil { t.Fatal(err) }
	if err := servers.Register(Server{Name: "jira", Client: &discoveryClient{tools: []mcp.ToolDefinition{{Name: "jira.search"}}}}); err != nil { t.Fatal(err) }

	got, err := NewDiscovery(servers).Discover(context.Background())
	if err != nil { t.Fatal(err) }
	if len(got) != 2 || got[0].Name != "github.search" || got[1].Name != "jira.search" {
		t.Fatalf("unexpected discovered tools: %+v", got)
	}
}

func TestDiscoveryRejectsToolNameCollision(t *testing.T) {
	servers := NewServerRegistry()
	if err := servers.Register(Server{Name: "one", Client: &discoveryClient{tools: []mcp.ToolDefinition{{Name: "search"}}}}); err != nil { t.Fatal(err) }
	if err := servers.Register(Server{Name: "two", Client: &discoveryClient{tools: []mcp.ToolDefinition{{Name: "search"}}}}); err != nil { t.Fatal(err) }

	if _, err := NewDiscovery(servers).Discover(context.Background()); err == nil {
		t.Fatal("expected duplicate tool error")
	}
}
