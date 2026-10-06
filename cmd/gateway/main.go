package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/hritik2899/mcp-context-gateway/internal/gateway"
	"github.com/hritik2899/mcp-context-gateway/internal/router"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
)

const serverVersion = "0.1.0"

func main() {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.Definition{
		Name:        "health.check",
		Description: "Returns the gateway health status.",
		InputSchema: map[string]any{"type": "object"},
	})

	executor := tools.NewLocalExecutor(registry)
	_ = executor.Register("health.check", func(ctx context.Context, arguments map[string]any) (any, error) {
		return map[string]any{"status": "ok"}, nil
	})

	routes := router.NewRouteRegistry()
	if err := routes.Register(router.ToolRoute{ToolName: "health.check", Backend: router.BackendLocal}); err != nil {
		log.Fatal(err)
	}

	servers := router.NewServerRegistry()
	catalog := router.NewCatalog(registry, executor, servers, router.DiscoveryOptions{})
	if err := catalog.Refresh(context.Background()); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.Handle("/mcp", gateway.NewHandler(catalog, catalog, gateway.Options{}))

	server := &http.Server{Addr: "127.0.0.1:8080", Handler: mux}
	log.Printf("mcp-context-gateway listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
