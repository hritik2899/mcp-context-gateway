package policy

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/gateway"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens  float64
	updated time.Time
}
type Policy struct {
	principals []config.Principal
	origins    map[string]bool
	mu         sync.Mutex
	rates      map[string]*bucket
	slots      chan struct{}
}

func New(c config.Config) *Policy {
	origins := map[string]bool{}
	for _, o := range c.AllowedOrigins {
		origins[o] = true
	}
	return &Policy{principals: c.Principals, origins: origins, rates: map[string]*bucket{}, slots: make(chan struct{}, c.MaxConcurrentRequests)}
}
func (p *Policy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if values := r.Header.Values("Origin"); len(values) > 1 || (len(values) == 1 && !p.origins[values[0]]) {
			http.Error(w, "origin not allowed", 403)
			return
		}
		identity := "local"
		limit := 120
		if len(p.principals) > 0 {
			identity = ""
			auth := r.Header.Values("Authorization")
			if len(auth) == 1 && strings.HasPrefix(auth[0], "Bearer ") {
				token := sha256.Sum256([]byte(strings.TrimPrefix(auth[0], "Bearer ")))
				for _, principal := range p.principals {
					expected := sha256.Sum256([]byte(principal.Token))
					if subtle.ConstantTimeCompare(token[:], expected[:]) == 1 {
						identity = principal.Name
						limit = principal.RequestsPerMinute
					}
				}
			}
			if identity == "" {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "authentication required", 401)
				return
			}
		}
		if !p.allowRate(identity, limit) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", 429)
			return
		}
		select {
		case p.slots <- struct{}{}:
			defer func() { <-p.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "gateway concurrency limit reached", 503)
			return
		}
		next.ServeHTTP(w, r.WithContext(gateway.WithIdentity(r.Context(), identity)))
	})
}
func (p *Policy) allowRate(identity string, limit int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	b := p.rates[identity]
	if b == nil {
		b = &bucket{tokens: float64(limit), updated: now}
		p.rates[identity] = b
	}
	b.tokens += now.Sub(b.updated).Seconds() * float64(limit) / 60
	if b.tokens > float64(limit) {
		b.tokens = float64(limit)
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
func matches(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}
func (p *Policy) Allowed(ctx context.Context, name string) bool {
	if len(p.principals) == 0 {
		return gateway.Identity(ctx) == "local"
	}
	for _, principal := range p.principals {
		if principal.Name != gateway.Identity(ctx) {
			continue
		}
		if matches(principal.DenyTools, name) || !matches(principal.AllowTools, name) {
			return false
		}
		if len(principal.AllowServers) > 0 {
			server, _, hasPrefix := strings.Cut(name, ".")
			if !hasPrefix {
				server = "local"
			}
			if name == "health.check" {
				server = "local"
			}
			return matches(principal.AllowServers, server)
		}
		return true
	}
	return false
}

// Service applies the same visibility rules to discovery and execution.
type Service struct {
	Policy   *Policy
	Catalog  gateway.Catalog
	Executor gateway.Executor
}

func (s *Service) List(ctx context.Context) ([]mcp.ToolDefinition, error) {
	tools, err := s.Catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]mcp.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if s.Policy.Allowed(ctx, tool.Name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered, nil
}
func (s *Service) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if !s.Policy.Allowed(ctx, name) {
		return nil, mcp.InvalidTool(name)
	}
	return s.Executor.Execute(ctx, name, args)
}
