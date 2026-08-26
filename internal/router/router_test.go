package router

import (
	"context"
	"testing"

	"github.com/hritik2899/mcp-context-gateway/internal/tools"
)

type fakeClient struct { called bool }

func (c *fakeClient) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	c.called = true
	return map[string]any{"tool": name}, nil
}

func TestRouterDispatchesLocalRoute(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{Name: "health.check"}); err != nil { t.Fatal(err) }
	executor := tools.NewLocalExecutor(registry)
	if err := executor.Register("health.check", func(ctx context.Context, arguments map[string]any) (any, error) {
		return map[string]any{"status": "ok"}, nil
	}); err != nil { t.Fatal(err) }
	routes := NewRouteRegistry()
	if err := routes.Register(ToolRoute{ToolName: "health.check", Backend: BackendLocal}); err != nil { t.Fatal(err) }

	r := New(executor, routes, NewServerRegistry())
	result, err := r.Execute(context.Background(), "health.check", nil)
	if err != nil { t.Fatal(err) }
	if result.(map[string]any)["status"] != "ok" { t.Fatalf("unexpected result: %+v", result) }
}

func TestRouterDispatchesRemoteRoute(t *testing.T) {
	client := &fakeClient{}
	servers := NewServerRegistry()
	if err := servers.Register(Server{Name: "github", Client: client}); err != nil { t.Fatal(err) }
	routes := NewRouteRegistry()
	if err := routes.Register(ToolRoute{ToolName: "github.search", Backend: BackendRemote, ServerName: "github"}); err != nil { t.Fatal(err) }

	r := New(nil, routes, servers)
	result, err := r.Execute(context.Background(), "github.search", map[string]any{"query": "mcp"})
	if err != nil { t.Fatal(err) }
	if !client.called { t.Fatal("expected downstream client to be called") }
	if result.(map[string]any)["tool"] != "github.search" { t.Fatalf("unexpected result: %+v", result) }
}

func TestRouterRejectsUnknownRoute(t *testing.T) {
	r := New(nil, NewRouteRegistry(), NewServerRegistry())
	if _, err := r.Execute(context.Background(), "missing", nil); err == nil { t.Fatal("expected unknown route error") }
}
