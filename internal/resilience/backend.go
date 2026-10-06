// Package resilience bounds work per downstream and isolates repeated failures.
package resilience

import (
	"context"
	"errors"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"sync"
	"time"
)

var ErrBusy = errors.New("downstream concurrency limit reached")
var ErrCircuitOpen = errors.New("downstream circuit open")

type Options struct {
	MaxConcurrent    int
	FailureThreshold int
	Cooldown         time.Duration
	Timeout          time.Duration
}
type Backend struct {
	client     mcp.Client
	options    Options
	slots      chan struct{}
	mu         sync.Mutex
	failures   int
	openUntil  time.Time
	probe      bool
	generation uint64
}

func New(client mcp.Client, o Options) *Backend {
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = 16
	}
	if o.FailureThreshold <= 0 {
		o.FailureThreshold = 5
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 15 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	return &Backend{client: client, options: o, slots: make(chan struct{}, o.MaxConcurrent)}
}
func (b *Backend) admit(ctx context.Context) (func(error), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case b.slots <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	b.mu.Lock()
	now := time.Now()
	if !b.openUntil.IsZero() {
		if now.Before(b.openUntil) || b.probe {
			b.mu.Unlock()
			<-b.slots
			return nil, ErrCircuitOpen
		}
		b.probe = true
	}
	generation := b.generation
	isProbe := b.probe
	b.mu.Unlock()
	return func(err error) {
		defer func() { <-b.slots }()
		b.mu.Lock()
		defer b.mu.Unlock()
		if generation != b.generation {
			return
		}
		var rpc *mcp.JSONRPCError
		failure := err != nil && !errors.Is(err, context.Canceled) && !errors.As(err, &rpc)
		if isProbe {
			b.probe = false
			if errors.Is(err, context.Canceled) {
				b.openUntil = time.Now().Add(b.options.Cooldown)
				return
			}
		}
		if failure {
			b.failures++
			if b.failures >= b.options.FailureThreshold || isProbe {
				b.openUntil = time.Now().Add(b.options.Cooldown)
				b.generation++
				b.probe = false
			}
		} else {
			b.failures = 0
			b.openUntil = time.Time{}
		}
	}, nil
}
func (b *Backend) CallTool(ctx context.Context, name string, args map[string]any) (result mcp.CallToolResult, err error) {
	finish, err := b.admit(ctx)
	if err != nil {
		return result, err
	}
	defer func() { finish(err) }()
	child, cancel := context.WithTimeout(ctx, b.options.Timeout)
	defer cancel()
	return b.client.CallTool(child, name, args)
}
func (b *Backend) ListTools(ctx context.Context) (result []mcp.ToolDefinition, err error) {
	finish, err := b.admit(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { finish(err) }()
	child, cancel := context.WithTimeout(ctx, b.options.Timeout)
	defer cancel()
	discoverer, ok := b.client.(mcp.ToolDiscoverer)
	if !ok {
		return nil, errors.New("backend does not support discovery")
	}
	return discoverer.ListTools(child)
}
