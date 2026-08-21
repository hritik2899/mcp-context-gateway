package router

import (
	"fmt"
	"sync"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
)

// Server describes a downstream MCP server available to the gateway.
type Server struct {
	Name   string
	Client mcp.Client
}

// ServerRegistry owns configured downstream MCP servers.
type ServerRegistry struct {
	mu      sync.RWMutex
	servers map[string]Server
}

func NewServerRegistry() *ServerRegistry {
	return &ServerRegistry{servers: make(map[string]Server)}
}

func (r *ServerRegistry) Register(server Server) error {
	if server.Name == "" {
		return fmt.Errorf("server name is required")
	}
	if server.Client == nil {
		return fmt.Errorf("client for server %q is required", server.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.servers[server.Name]; exists {
		return fmt.Errorf("server %q is already registered", server.Name)
	}
	r.servers[server.Name] = server
	return nil
}

func (r *ServerRegistry) Lookup(name string) (Server, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	server, ok := r.servers[name]
	return server, ok
}
