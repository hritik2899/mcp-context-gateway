package router

import (
	"context"
	"errors"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
	"sync"
	"testing"
)

type discoveryClient struct {
	mu     sync.Mutex
	fail   bool
	called string
}

func (c *discoveryClient) CallTool(ctx context.Context, name string, args map[string]any) (mcp.CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.called = name
	return mcp.TextResult(name, false), nil
}
func (c *discoveryClient) ListTools(context.Context) ([]mcp.ToolDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return nil, errors.New("down")
	}
	return []mcp.ToolDefinition{{Name: "search", InputSchema: map[string]any{"type": "object", "required": []any{"query"}, "properties": map[string]any{"query": map[string]any{"type": "string"}}}}}, nil
}
func TestCatalogRoutesFailureIsolationAndRemoval(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(tools.Definition{Name: "health.check", InputSchema: map[string]any{"type": "object"}}); err != nil {
		t.Fatal(err)
	}
	local := tools.NewLocalExecutor(reg)
	local.Register("health.check", func(context.Context, map[string]any) (any, error) { return "ok", nil })
	servers := NewServerRegistry()
	a, b := &discoveryClient{}, &discoveryClient{}
	servers.Register(Server{Name: "one", Client: a})
	servers.Register(Server{Name: "two", Client: b})
	c := NewCatalog(reg, local, servers, DiscoveryOptions{})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ := c.List(context.Background())
	if len(list) != 3 || list[1].Name != "one.search" || list[2].Name != "two.search" {
		t.Fatal(list)
	}
	result, err := c.Execute(context.Background(), "one.search", map[string]any{"query": "test"})
	if err != nil || result.(mcp.CallToolResult).IsError || a.called != "search" {
		t.Fatal(result, err)
	}
	result, err = c.Execute(context.Background(), "one.search", map[string]any{"query": 42})
	if err != nil || !result.(mcp.CallToolResult).IsError {
		t.Fatal("schema validation missing")
	}
	a.mu.Lock()
	a.fail = true
	a.mu.Unlock()
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ = c.List(context.Background())
	if len(list) != 2 || c.Ready() {
		t.Fatal(list)
	}
	if _, err := c.Execute(context.Background(), "two.search", map[string]any{"query": "still works"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RemoveServer(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(context.Background(), "two.search", nil); err == nil {
		t.Fatal("removed route remains")
	}
}
func TestCatalogConcurrentRefreshAndCalls(t *testing.T) {
	reg := tools.NewRegistry()
	servers := NewServerRegistry()
	servers.Register(Server{Name: "one", Client: &discoveryClient{}})
	c := NewCatalog(reg, tools.NewLocalExecutor(reg), servers, DiscoveryOptions{})
	c.Refresh(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				c.Refresh(context.Background())
				c.List(context.Background())
				c.Execute(context.Background(), "one.search", map[string]any{"query": "test"})
			}
		}()
	}
	wg.Wait()
}
