package app

import (
	"context"
	"encoding/json"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, offline *atomic.Bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var req mcp.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch req.Method {
		case mcp.InitializeMethod:
			w.Header().Set("MCP-Session-Id", "fixture-session")
			result = mcp.InitializeResult{ProtocolVersion: mcp.ProtocolVersion, Capabilities: map[string]any{"tools": map[string]any{}}, ServerInfo: mcp.ServerInfo{Name: "fixture", Version: "1"}}
		case mcp.InitializedNotification:
			w.WriteHeader(202)
			return
		case mcp.ToolsListMethod:
			result = mcp.ToolsListResult{Tools: []mcp.ToolDefinition{{Name: "search", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []any{"query"}}}}}
		case mcp.ToolsCallMethod:
			var p mcp.CallToolParams
			json.Unmarshal(req.Params, &p)
			if p.Name != "search" {
				t.Error("namespaced alias was not translated")
			}
			result = mcp.TextResult("fixture reply", false)
		default:
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	}))
}
func TestMultiServerGatewayAndFailureIsolation(t *testing.T) {
	var offlineA, offlineB atomic.Bool
	a, b := fixture(t, &offlineA), fixture(t, &offlineB)
	defer a.Close()
	defer b.Close()
	c := config.Default()
	c.RefreshInterval = config.Duration(time.Hour)
	c.Servers = []config.Server{{Name: "one", URL: a.URL}, {Name: "two", URL: b.URL}}
	application, err := New(context.Background(), c, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close(context.Background())
	server := httptest.NewServer(application.Handler)
	defer server.Close()
	client := mcp.NewHTTPClient(server.URL + "/mcp")
	defer client.Close(context.Background())
	list, err := client.ListTools(context.Background())
	if err != nil || len(list) != 3 {
		t.Fatal(list, err)
	}
	for _, name := range []string{"one.search", "two.search"} {
		result, err := client.CallTool(context.Background(), name, map[string]any{"query": "example"})
		if err != nil || result.IsError || result.Content[0].Text != "fixture reply" {
			t.Fatal(result, err)
		}
	}
	invalid, err := client.CallTool(context.Background(), "one.search", map[string]any{"query": 42})
	if err != nil || !invalid.IsError {
		t.Fatal("invalid arguments reached downstream")
	}
	offlineA.Store(true)
	if err := application.Catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err = client.ListTools(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	if _, err := client.CallTool(context.Background(), "two.search", map[string]any{"query": "still works"}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal("degraded gateway reports ready")
	}
	resp, err = http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("liveness failed")
	}
}
func TestAuthenticatedGatewayFiltersTools(t *testing.T) {
	const token = "01234567890123456789012345678901"
	t.Setenv("GATEWAY_TEST_READER", token)
	c := config.Default()
	c.Principals = []config.Principal{{Name: "reader", TokenEnv: "GATEWAY_TEST_READER", AllowTools: []string{}}}
	application, err := New(context.Background(), c, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close(context.Background())
	server := httptest.NewServer(application.Handler)
	defer server.Close()
	client := mcp.NewHTTPClientWithOptions(server.URL+"/mcp", mcp.ClientOptions{BearerToken: token})
	defer client.Close(context.Background())
	list, err := client.ListTools(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	if _, err := client.CallTool(context.Background(), "health.check", nil); err == nil {
		t.Fatal("denied tool executed")
	}
	resp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("metrics are public")
	}
}
