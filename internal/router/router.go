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

// Router resolves a tool name to the appropriate execution backend.
type Router struct {
	local *tools.LocalExecutor
}

func New(local *tools.LocalExecutor) *Router {
	return &Router{local: local}
}

func (r *Router) Execute(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if r.local == nil {
		return nil, fmt.Errorf("local execution backend is unavailable")
	}
	return r.local.Execute(ctx, name, arguments)
}
