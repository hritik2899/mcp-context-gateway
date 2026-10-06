package resilience

import (
	"context"
	"errors"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"testing"
	"time"
)

type client struct {
	fail    bool
	block   chan struct{}
	started chan struct{}
}

func (c *client) CallTool(ctx context.Context, _ string, _ map[string]any) (mcp.CallToolResult, error) {
	if c.started != nil {
		close(c.started)
	}
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return mcp.CallToolResult{}, ctx.Err()
		}
	}
	if c.fail {
		return mcp.CallToolResult{}, errors.New("offline")
	}
	return mcp.TextResult("ok", false), nil
}
func TestCircuitOpensAndRecovers(t *testing.T) {
	c := &client{fail: true}
	b := New(c, Options{FailureThreshold: 1, Cooldown: time.Hour})
	b.CallTool(context.Background(), "test", nil)
	if _, err := b.CallTool(context.Background(), "test", nil); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.openUntil = time.Now().Add(-time.Second)
	b.mu.Unlock()
	c.fail = false
	if _, err := b.CallTool(context.Background(), "test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CallTool(context.Background(), "test", nil); err != nil {
		t.Fatal(err)
	}
}
func TestBulkheadRejectsOverflow(t *testing.T) {
	c := &client{block: make(chan struct{}), started: make(chan struct{})}
	b := New(c, Options{MaxConcurrent: 1})
	done := make(chan struct{})
	go func() { defer close(done); b.CallTool(context.Background(), "test", nil) }()
	<-c.started
	if _, err := b.CallTool(context.Background(), "test", nil); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	close(c.block)
	<-done
}
