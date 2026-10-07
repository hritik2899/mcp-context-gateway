package router

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
	"github.com/hritik2899/mcp-context-gateway/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type DiscoveryOptions struct {
	Concurrency int
	Timeout     time.Duration
	Observe     func(context.Context, string, string, bool, time.Duration)
}
type entry struct {
	definition mcp.ToolDefinition
	schema     *jsonschema.Schema
	remoteName string
	server     Server
}
type ServerHealth struct {
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
	ToolCount int       `json:"toolCount"`
}

// Catalog publishes discovery and execution routes as one immutable snapshot.
// A failed server is removed from discovery; healthy servers and local tools stay usable.
type Catalog struct {
	local    *tools.Registry
	executor *tools.LocalExecutor
	servers  *ServerRegistry
	options  DiscoveryOptions
	refresh  chan struct{}
	mu       sync.RWMutex
	entries  map[string]entry
	health   []ServerHealth
}

func NewCatalog(local *tools.Registry, executor *tools.LocalExecutor, servers *ServerRegistry, options DiscoveryOptions) *Catalog {
	if options.Concurrency <= 0 {
		options.Concurrency = 4
	}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	return &Catalog{local: local, executor: executor, servers: servers, options: options, refresh: make(chan struct{}, 1), entries: map[string]entry{}}
}
func (c *Catalog) List(ctx context.Context) ([]mcp.ToolDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]mcp.ToolDefinition, 0, len(c.entries))
	for _, e := range c.entries {
		result = append(result, e.definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	// Never share mutable schema maps with callers.
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var copy []mcp.ToolDefinition
	err = json.Unmarshal(data, &copy)
	return copy, err
}
func (c *Catalog) Execute(ctx context.Context, name string, arguments map[string]any) (result any, err error) {
	c.mu.RLock()
	e, ok := c.entries[name]
	c.mu.RUnlock()
	if !ok {
		return nil, mcp.InvalidTool(name)
	}
	start := time.Now()
	defer func() {
		if c.options.Observe != nil {
			failed := err != nil
			if r, ok := result.(mcp.CallToolResult); ok {
				failed = failed || r.IsError
			}
			server := e.server.Name
			if server == "" {
				server = "local"
			}
			c.options.Observe(ctx, server, name, failed, time.Since(start))
		}
	}()
	if arguments == nil {
		arguments = map[string]any{}
	}
	if err := e.schema.Validate(arguments); err != nil {
		return mcp.TextResult("arguments do not match the tool input schema", true), nil
	}
	if e.remoteName == "" {
		return c.executor.Execute(ctx, name, arguments)
	}
	return e.server.Client.CallTool(ctx, e.remoteName, arguments)
}
func (c *Catalog) Health() []ServerHealth {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]ServerHealth(nil), c.health...)
}
func (c *Catalog) Ready() bool {
	for _, s := range c.Health() {
		if !s.Healthy {
			return false
		}
	}
	return true
}
func (c *Catalog) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Refresh(ctx)
		}
	}
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func makeEntry(definition mcp.ToolDefinition, server Server) (entry, error) {
	if !toolName.MatchString(definition.Name) {
		return entry{}, fmt.Errorf("invalid tool name")
	}
	schema, err := validation.Compile(definition.InputSchema)
	if err != nil {
		return entry{}, err
	}
	e := entry{definition: definition, schema: schema, server: server}
	if server.Name != "" {
		e.remoteName = definition.Name
		e.definition.Name = server.Name + "." + definition.Name
		if !toolName.MatchString(e.definition.Name) {
			return entry{}, fmt.Errorf("namespaced tool name exceeds limit")
		}
	}
	return e, nil
}
