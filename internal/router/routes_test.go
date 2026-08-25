package router

import "testing"

func TestRouteRegistry(t *testing.T) {
	registry := NewRouteRegistry()

	if err := registry.Register(ToolRoute{ToolName: "health.check", Backend: BackendLocal}); err != nil {
		t.Fatalf("register local route: %v", err)
	}
	if err := registry.Register(ToolRoute{ToolName: "github.search", Backend: BackendRemote, ServerName: "github"}); err != nil {
		t.Fatalf("register remote route: %v", err)
	}

	route, ok := registry.Lookup("github.search")
	if !ok || route.Backend != BackendRemote || route.ServerName != "github" {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestRouteRegistryRejectsInvalidRoutes(t *testing.T) {
	registry := NewRouteRegistry()

	cases := []ToolRoute{
		{Backend: BackendLocal},
		{ToolName: "unknown", Backend: Backend("broken")},
		{ToolName: "remote", Backend: BackendRemote},
	}
	for _, route := range cases {
		if err := registry.Register(route); err == nil {
			t.Fatalf("expected invalid route to be rejected: %+v", route)
		}
	}
}
