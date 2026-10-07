package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestRegistryOwnsSchemaCopies(t *testing.T) {
	r := NewRegistry()
	schema := map[string]any{"type": "object"}
	if err := r.Register(Definition{Name: "test", InputSchema: schema}); err != nil {
		t.Fatal(err)
	}
	schema["type"] = "array"
	d, _ := r.Lookup("test")
	d.InputSchema["type"] = "string"
	if r.List()[0].InputSchema["type"] != "object" {
		t.Fatal("schema escaped by reference")
	}
}
func TestConcurrentRegistrationAndExecution(t *testing.T) {
	r := NewRegistry()
	e := NewLocalExecutor(r)
	r.Register(Definition{Name: "base", InputSchema: map[string]any{"type": "object"}})
	e.Register("base", func(context.Context, map[string]any) (any, error) { return "ok", nil })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("tool%d", i)
			r.Register(Definition{Name: name, InputSchema: map[string]any{"type": "object"}})
			e.Register(name, func(context.Context, map[string]any) (any, error) { return "ok", nil })
			if _, err := e.Execute(context.Background(), "base", nil); err != nil {
				t.Error(err)
			}
			r.List()
		}(i)
	}
	wg.Wait()
}
