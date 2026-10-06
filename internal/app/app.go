// Package app composes the configured gateway without global HTTP state.
package app

import (
	"context"
	"errors"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/gateway"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"github.com/hritik2899/mcp-context-gateway/internal/observability"
	"github.com/hritik2899/mcp-context-gateway/internal/policy"
	"github.com/hritik2899/mcp-context-gateway/internal/resilience"
	"github.com/hritik2899/mcp-context-gateway/internal/router"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type App struct {
	Handler   http.Handler
	Catalog   *router.Catalog
	MCP       *gateway.Handler
	clients   []*mcp.HTTPClient
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

func New(ctx context.Context, c config.Config, logger *slog.Logger) (*App, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{done: make(chan struct{})}
	registry := tools.NewRegistry()
	if err := registry.Register(tools.Definition{Name: "health.check", Description: "Returns gateway liveness.", InputSchema: map[string]any{"type": "object", "additionalProperties": false}}); err != nil {
		return nil, err
	}
	local := tools.NewLocalExecutor(registry)
	if err := local.Register("health.check", func(context.Context, map[string]any) (any, error) { return map[string]any{"status": "ok"}, nil }); err != nil {
		return nil, err
	}
	metrics := observability.New(logger)
	servers := router.NewServerRegistry()
	for _, s := range c.Servers {
		client := mcp.NewHTTPClientWithOptions(s.URL, mcp.ClientOptions{Timeout: s.Timeout.Value(), MaxResponseBytes: c.MaxResponseBytes, BearerToken: s.Token})
		a.clients = append(a.clients, client)
		backend := resilience.New(client, resilience.Options{MaxConcurrent: s.MaxConcurrent, FailureThreshold: s.FailureThreshold, Cooldown: s.Cooldown.Value(), Timeout: s.Timeout.Value()})
		if err := servers.Register(router.Server{Name: s.Name, Client: backend}); err != nil {
			return nil, err
		}
	}
	a.Catalog = router.NewCatalog(registry, local, servers, router.DiscoveryOptions{Concurrency: c.DiscoveryConcurrency, Timeout: c.DiscoveryTimeout.Value(), Observe: func(ctx context.Context, server, name string, failed bool, elapsed time.Duration) {
		metrics.ObserveTool(name, failed, elapsed)
		logger.Info("tool call", "request_id", observability.RequestID(ctx), "principal", gateway.Identity(ctx), "server", server, "tool", name, "failed", failed, "duration_ms", elapsed.Milliseconds())
	}})
	if err := a.Catalog.Refresh(ctx); err != nil {
		for _, client := range a.clients {
			client.Close(ctx)
		}
		return nil, err
	}
	access := policy.New(c)
	service := &policy.Service{Policy: access, Catalog: a.Catalog, Executor: a.Catalog}
	a.MCP = gateway.NewHandler(service, service, gateway.Options{MaxBodyBytes: c.MaxRequestBytes, RequestTimeout: c.RequestTimeout.Value(), SessionTTL: c.SessionTTL.Value(), MaxSessions: c.MaxSessions})
	mux := http.NewServeMux()
	mux.Handle("/mcp", access.Middleware(a.MCP))
	mux.Handle("GET /metrics", access.Middleware(metrics))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !a.Catalog.Ready() {
			w.WriteHeader(503)
			w.Write([]byte(`{"status":"degraded"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	a.Handler = metrics.Middleware(mux)
	background, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go func() { defer close(a.done); a.Catalog.Run(background, c.RefreshInterval.Value()) }()
	return a, nil
}
func (a *App) Close(ctx context.Context) error {
	var result error
	a.closeOnce.Do(func() {
		a.cancel()
		select {
		case <-a.done:
		case <-ctx.Done():
			result = ctx.Err()
		}
		a.MCP.Close()
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, client := range a.clients {
			wg.Add(1)
			go func(client *mcp.HTTPClient) {
				defer wg.Done()
				if err := client.Close(ctx); err != nil {
					mu.Lock()
					result = errors.Join(result, err)
					mu.Unlock()
				}
			}(client)
		}
		wg.Wait()
	})
	return result
}
