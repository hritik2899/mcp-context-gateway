package policy

import (
	"context"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/gateway"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticationOriginAndRateLimits(t *testing.T) {
	c := config.Default()
	c.Principals = []config.Principal{{Name: "reader", Token: "test-token", RequestsPerMinute: 1}}
	p := New(c)
	handler := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gateway.Identity(r.Context()) != "reader" {
			t.Error("identity missing")
		}
		w.WriteHeader(204)
	}))
	for _, tc := range []struct {
		auth, origin string
		status       int
	}{{"", "", 401}, {"Bearer test-token", "https://evil.example", 403}, {"Bearer test-token", "", 204}, {"Bearer test-token", "", 429}} {
		r := httptest.NewRequest("POST", "/mcp", nil)
		r.Header.Set("Authorization", tc.auth)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
}
func TestPolicyMatchesDiscoveryAndCalls(t *testing.T) {
	c := config.Default()
	c.Principals = []config.Principal{{Name: "reader", AllowTools: []string{"one.*", "two.*"}, DenyTools: []string{"*.delete"}, AllowServers: []string{"one"}}}
	p := New(c)
	ctx := gateway.WithIdentity(context.Background(), "reader")
	if !p.Allowed(ctx, "one.search") || p.Allowed(ctx, "one.delete") || p.Allowed(ctx, "two.search") || p.Allowed(context.Background(), "one.search") {
		t.Fatal("incorrect policy")
	}
}
