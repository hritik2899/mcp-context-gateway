package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

type sample struct {
	count   uint64
	errors  uint64
	seconds float64
}
type Metrics struct {
	mu       sync.Mutex
	requests sample
	tools    map[string]sample
	logger   *slog.Logger
}

func New(logger *slog.Logger) *Metrics { return &Metrics{tools: map[string]sample{}, logger: logger} }
func (m *Metrics) ObserveTool(name string, failed bool, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tools[name]; !ok && len(m.tools) >= 1000 {
		name = "other"
	}
	s := m.tools[name]
	s.count++
	if failed {
		s.errors++
	}
	s.seconds += duration.Seconds()
	m.tools[name] = s
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
		r.ResponseWriter.WriteHeader(code)
	}
}
func (r *recorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(200)
	}
	return r.ResponseWriter.Write(body)
}
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var token [16]byte
		rand.Read(token[:])
		id := hex.EncodeToString(token[:])
		w.Header().Set("X-Request-ID", id)
		out := &recorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				http.Error(out, "internal server error", 500)
				m.logger.Error("request panic", "request_id", id)
			}
			if out.status == 0 {
				out.status = 200
			}
			duration := time.Since(start)
			m.mu.Lock()
			m.requests.count++
			if out.status >= 400 {
				m.requests.errors++
			}
			m.requests.seconds += duration.Seconds()
			m.mu.Unlock()
			route := r.URL.Path
			if route != "/mcp" && route != "/healthz" && route != "/readyz" && route != "/metrics" {
				route = "unknown"
			}
			m.logger.Info("http request", "request_id", id, "method", r.Method, "route", route, "status", out.status, "duration_ms", duration.Milliseconds())
		}()
		next.ServeHTTP(out, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func (m *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE gateway_http_requests_total counter\ngateway_http_requests_total %d\n# TYPE gateway_http_errors_total counter\ngateway_http_errors_total %d\ngateway_http_duration_seconds_sum %g\ngateway_http_duration_seconds_count %d\n", m.requests.count, m.requests.errors, m.requests.seconds, m.requests.count)
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := m.tools[name]
		fmt.Fprintf(w, "gateway_tool_calls_total{tool=%q} %d\ngateway_tool_errors_total{tool=%q} %d\ngateway_tool_duration_seconds_sum{tool=%q} %g\ngateway_tool_duration_seconds_count{tool=%q} %d\n", name, s.count, name, s.errors, name, s.seconds, name, s.count)
	}
}

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}
