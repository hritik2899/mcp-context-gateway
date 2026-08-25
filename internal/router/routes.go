package router

import (
	"fmt"
	"sync"
)

type Backend string

const (
	BackendLocal  Backend = "local"
	BackendRemote Backend = "remote"
)

type ToolRoute struct {
	ToolName  string
	Backend   Backend
	ServerName string
}

type RouteRegistry struct {
	mu     sync.RWMutex
	routes map[string]ToolRoute
}

func NewRouteRegistry() *RouteRegistry {
	return &RouteRegistry{routes: make(map[string]ToolRoute)}
}

func (r *RouteRegistry) Register(route ToolRoute) error {
	if route.ToolName == "" {
		return fmt.Errorf("tool name is required")
	}
	if route.Backend != BackendLocal && route.Backend != BackendRemote {
		return fmt.Errorf("unsupported backend %q", route.Backend)
	}
	if route.Backend == BackendRemote && route.ServerName == "" {
		return fmt.Errorf("server name is required for remote route")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routes[route.ToolName]; exists {
		return fmt.Errorf("route for tool %q is already registered", route.ToolName)
	}
	r.routes[route.ToolName] = route
	return nil
}

func (r *RouteRegistry) Lookup(toolName string) (ToolRoute, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[toolName]
	return route, ok
}
