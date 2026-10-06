package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
)

type stub struct {
	result  any
	wait    bool
	started chan struct{}
}

func (s *stub) List(context.Context) ([]mcp.ToolDefinition, error) {
	return []mcp.ToolDefinition{{Name: "test", InputSchema: map[string]any{"type": "object"}}}, nil
}
func (s *stub) Execute(ctx context.Context, _ string, _ map[string]any) (any, error) {
	if s.wait {
		if s.started != nil {
			close(s.started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.result, nil
}
func request(h *Handler, sid, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("MCP-Session-Id", sid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func initialize(t *testing.T, h *Handler) string {
	t.Helper()
	w := request(h, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"unsupported","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	sid := w.Header().Get("MCP-Session-Id")
	if sid == "" || !strings.Contains(w.Body.String(), mcp.ProtocolVersion) {
		t.Fatal(w.Body.String())
	}
	w = request(h, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w.Code != 202 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
	return sid
}
func TestProtocolValidation(t *testing.T) {
	s := &stub{}
	h := NewHandler(s, s, Options{})
	sid := initialize(t, h)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{`, mcp.ParseError}, {`[]`, mcp.InvalidRequest}, {`{"jsonrpc":"2.0","id":{},"method":"ping"}`, mcp.InvalidRequest},
		{`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":[]}`, mcp.InvalidParams},
		{`{"jsonrpc":"2.0","id":2,"method":"unknown"}`, mcp.MethodNotFound},
	} {
		w := request(h, sid, tc.body)
		var reply mcp.JSONRPCResponse
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Error == nil || reply.Error.Code != tc.code {
			t.Fatalf("%s => %s", tc.body, w.Body.String())
		}
	}
	w := request(h, sid, `{"jsonrpc":"2.0","method":"tools/list"}`)
	if w.Code != 202 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
	w = request(h, "", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if w.Code != 404 {
		t.Fatal(w)
	}
}
func TestResultPreserved(t *testing.T) {
	raw := `{"content":[{"type":"image","data":"YWJj","mimeType":"image/png"}],"isError":true,"structuredContent":{"value":7},"_meta":{"source":"upstream"},"extension":true}`
	var result mcp.CallToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	s := &stub{result: result}
	h := NewHandler(s, s, Options{})
	sid := initialize(t, h)
	w := request(h, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"test"}}`)
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	json.Unmarshal(w.Body.Bytes(), &reply)
	if string(reply.Result) != raw {
		t.Fatalf("changed result: %s", reply.Result)
	}
}
func TestCancellation(t *testing.T) {
	s := &stub{wait: true, started: make(chan struct{})}
	h := NewHandler(s, s, Options{})
	sid := initialize(t, h)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(h, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"test"}}`)
	}()
	<-s.started
	request(h, sid, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
	select {
	case w := <-done:
		if !strings.Contains(w.Body.String(), "cancelled") {
			t.Fatal(w)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not propagate")
	}
}
func TestBodyLimit(t *testing.T) {
	s := &stub{}
	h := NewHandler(s, s, Options{MaxBodyBytes: 10})
	w := request(h, "", strings.Repeat("x", 11))
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
}
func TestSessionBoundToIdentity(t *testing.T) {
	s := &stub{}
	h := NewHandler(s, s, Options{})
	sid := initialize(t, h)
	r := httptest.NewRequest("DELETE", "/mcp", nil)
	r.Header.Set("MCP-Session-Id", sid)
	r = r.WithContext(WithIdentity(r.Context(), "another-user"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	s := &stub{result: mcp.TextResult(strings.Repeat("x", 4096), false)}
	h := NewHandler(s, s, Options{MaxResponseBytes: 1024})
	sid := initialize(t, h)
	w := request(h, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"test"}}`)
	if w.Body.Len() > 1024 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatal(w.Body.String())
	}
}
