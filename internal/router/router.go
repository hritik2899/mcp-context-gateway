package router

import (
	"context"
	"fmt"

	"github.com/hritik2899/mcp-context-gateway/internal/tools"
)

// Route describes where a tool is executed.
type Route interface {
	Execute(ctx context.Context, name string, arguments map[string]any) (any, error)
}

// Router resolves a tool name to a local or downstream MCP execution backend.
type Router struct {
	local   *tools.LocalExecutor
	routes  *RouteRegistry
	servers *ServerRegistry
}

func New(local *tools.LocalExecutor, routes *RouteRegistry, servers *ServerRegistry) *Router {
	return &Router{local: local, routes: routes, servers: servers}
}

func (r *Router) Execute(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if r.routes == nil {
		return nil, fmt.Errorf("route registry is unavailable")
	}

	route, ok := r.routes.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("no route configured for tool %q", name)
	}

	switch route.Backend {
	case BackendLocal:
		if r.local == nil {
			return nil, fmt.Errorf("local execution backend is unavailable")
		}
		return r.local.Execute(ctx, name, arguments)
	case BackendRemote:
		if r.servers == nil {
			return nil, fmt.Errorf("server registry is unavailable")
		}
		server, ok := r.servers.Lookup(route.ServerName)
		if !ok {
			return nil, fmt.Errorf("downstream server %q is not registered", route.ServerName)
		}
		result, err := server.Client.CallTool(ctx, name, arguments)
		if err != nil {
			return nil, fmt.Errorf("execute tool %q on server %q: %w", name, route.ServerName, err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported backend %q", route.Backend)
	}
}
