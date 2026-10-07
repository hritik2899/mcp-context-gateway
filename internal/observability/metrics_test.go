package observability

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsAndLogsExcludeRequestPayload(t *testing.T) {
	var logs bytes.Buffer
	m := New(slog.New(slog.NewJSONHandler(&logs, nil)))
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Error("missing correlation ID")
		}
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader("sensitive payload"))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") == "" || strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "sensitive") {
		t.Fatal(logs.String())
	}
	m.ObserveTool("one.search", true, time.Second)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `gateway_tool_errors_total{tool="one.search"} 1`) {
		t.Fatal(w.Body.String())
	}
}
