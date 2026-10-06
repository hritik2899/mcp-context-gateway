package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func downstream(t *testing.T, fn func(http.ResponseWriter, *http.Request, JSONRPCRequest)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Error("missing Accept")
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Method == InitializeMethod {
			w.Header().Set("MCP-Session-Id", "test-session")
			json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: InitializeResult{ProtocolVersion: ProtocolVersion, Capabilities: map[string]any{"tools": map[string]any{}}, ServerInfo: ServerInfo{Name: "fixture", Version: "1"}}})
			return
		}
		if r.Header.Get("MCP-Session-Id") != "test-session" || r.Header.Get("MCP-Protocol-Version") != ProtocolVersion {
			t.Error("missing negotiated headers")
		}
		if req.Method == InitializedNotification || req.Method == "notifications/cancelled" {
			w.WriteHeader(202)
			return
		}
		fn(w, r, req)
	}))
}
func TestHTTPClientLifecyclePaginationAndSSE(t *testing.T) {
	server := downstream(t, func(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
		if req.Method == ToolsListMethod {
			var p struct {
				Cursor string `json:"cursor"`
			}
			json.Unmarshal(req.Params, &p)
			result := ToolsListResult{Tools: []ToolDefinition{{Name: "first", InputSchema: map[string]any{"type": "object"}}}, NextCursor: "page2"}
			if p.Cursor == "page2" {
				result.Tools[0].Name = "second"
				result.NextCursor = ""
			}
			json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"denied\"}],\"isError\":true}}\n\n", req.ID)
	})
	defer server.Close()
	c := NewHTTPClient(server.URL)
	defer c.Close(context.Background())
	tools, err := c.ListTools(context.Background())
	if err != nil || len(tools) != 2 {
		t.Fatalf("%+v %v", tools, err)
	}
	result, err := c.CallTool(context.Background(), "first", nil)
	if err != nil || !result.IsError || result.Content[0].Text != "denied" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestHTTPClientDoesNotReplayWrites(t *testing.T) {
	var calls atomic.Int32
	server := downstream(t, func(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) { calls.Add(1); w.WriteHeader(404) })
	defer server.Close()
	c := NewHTTPClient(server.URL)
	_, err := c.CallTool(context.Background(), "write", nil)
	if err != ErrSessionExpired || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
func TestHTTPClientValidatesCorrelationAndLimits(t *testing.T) {
	for _, mode := range []string{"wrong-id", "oversize", "cursor-loop"} {
		t.Run(mode, func(t *testing.T) {
			server := downstream(t, func(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
				switch mode {
				case "wrong-id":
					json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: json.RawMessage(`9999`), Result: TextResult("ok", false)})
				case "oversize":
					for i := 0; i < 3000; i++ {
						fmt.Fprint(w, " ")
					}
				case "cursor-loop":
					json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: ToolsListResult{Tools: []ToolDefinition{}, NextCursor: "again"}})
				}
			})
			defer server.Close()
			c := NewHTTPClientWithOptions(server.URL, ClientOptions{MaxResponseBytes: 1024})
			var err error
			if mode == "cursor-loop" {
				_, err = c.ListTools(context.Background())
			} else {
				_, err = c.CallTool(context.Background(), "test", nil)
			}
			if err == nil {
				t.Fatal("expected invalid downstream response rejection")
			}
		})
	}
}
func TestHTTPClientDeadline(t *testing.T) {
	server := downstream(t, func(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) { <-r.Context().Done() })
	defer server.Close()
	c := NewHTTPClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.CallTool(ctx, "slow", nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
}
