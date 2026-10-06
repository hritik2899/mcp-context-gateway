package router

import (
	"context"
	"fmt"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"sync"
	"time"
)

func (c *Catalog) Refresh(ctx context.Context) error {
	select {
	case c.refresh <- struct{}{}:
		defer func() { <-c.refresh }()
	case <-ctx.Done():
		return ctx.Err()
	}
	next := map[string]entry{}
	for _, tool := range c.local.List() {
		e, err := makeEntry(mcp.ToolDefinition{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema}, Server{})
		if err != nil {
			return err
		}
		next[tool.Name] = e
	}
	servers := c.servers.List()
	health := make([]ServerHealth, len(servers))
	discovered := make([]map[string]entry, len(servers))
	gate := make(chan struct{}, c.options.Concurrency)
	var wg sync.WaitGroup
	for i, server := range servers {
		wg.Add(1)
		go func(i int, server Server) {
			defer wg.Done()
			health[i] = ServerHealth{Name: server.Name, CheckedAt: time.Now()}
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				health[i].Error = "discovery cancelled"
				return
			}
			child, cancel := context.WithTimeout(ctx, c.options.Timeout)
			defer cancel()
			discoverer, ok := server.Client.(mcp.ToolDiscoverer)
			if !ok {
				health[i].Error = "tool discovery unsupported"
				return
			}
			definitions, err := discoverer.ListTools(child)
			if err != nil {
				health[i].Error = "downstream discovery failed"
				return
			}
			entries := map[string]entry{}
			for _, def := range definitions {
				e, err := makeEntry(def, server)
				if err != nil {
					health[i].Error = "invalid downstream tool definition"
					return
				}
				if _, exists := entries[e.definition.Name]; exists {
					health[i].Error = "duplicate downstream tool name"
					return
				}
				entries[e.definition.Name] = e
			}
			discovered[i] = entries
			health[i].Healthy = true
			health[i].ToolCount = len(entries)
		}(i, server)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, entries := range discovered {
		collision := false
		for name := range entries {
			if _, exists := next[name]; exists {
				collision = true
				break
			}
		}
		if collision {
			health[i].Healthy = false
			health[i].Error = "tool name collides with existing catalog"
			health[i].ToolCount = 0
			continue
		}
		for name, e := range entries {
			next[name] = e
		}
	}
	c.mu.Lock()
	c.entries = next
	c.health = health
	c.mu.Unlock()
	return nil
}

// RemoveServer withdraws advertised routes first. Already admitted calls may finish.
// Callers own closing the returned client after their shutdown grace period.
func (c *Catalog) RemoveServer(ctx context.Context, name string) (Server, error) {
	select {
	case c.refresh <- struct{}{}:
		defer func() { <-c.refresh }()
	case <-ctx.Done():
		return Server{}, ctx.Err()
	}
	server, ok := c.servers.Remove(name)
	if !ok {
		return Server{}, fmt.Errorf("unknown server %q", name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := map[string]entry{}
	for key, e := range c.entries {
		if e.server.Name != name {
			next[key] = e
		}
	}
	c.entries = next
	health := []ServerHealth{}
	for _, h := range c.health {
		if h.Name != name {
			health = append(health, h)
		}
	}
	c.health = health
	return server, nil
}
