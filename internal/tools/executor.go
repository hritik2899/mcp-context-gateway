package tools

import (
	"context"
	"fmt"
)

// Executor executes a registered tool by name.
type Executor interface {
	Execute(ctx context.Context, name string, arguments map[string]any) (any, error)
}

// Handler is the function used to execute a tool.
type Handler func(context.Context, map[string]any) (any, error)

// LocalExecutor keeps tool execution separate from tool discovery.
type LocalExecutor struct {
	registry *Registry
	handlers map[string]Handler
}

func NewLocalExecutor(registry *Registry) *LocalExecutor {
	return &LocalExecutor{registry: registry, handlers: make(map[string]Handler)}
}

func (e *LocalExecutor) Register(name string, handler Handler) error {
	if _, ok := e.registry.Lookup(name); !ok {
		return fmt.Errorf("tool %q is not registered", name)
	}
	if handler == nil {
		return fmt.Errorf("handler for tool %q is required", name)
	}
	if _, exists := e.handlers[name]; exists {
		return fmt.Errorf("handler for tool %q is already registered", name)
	}
	e.handlers[name] = handler
	return nil
}

func (e *LocalExecutor) Execute(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if _, ok := e.registry.Lookup(name); !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	handler, ok := e.handlers[name]
	if !ok {
		return nil, fmt.Errorf("tool %q has no executor", name)
	}
	return handler(ctx, arguments)
}
