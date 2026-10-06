package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Definition describes a tool exposed through the gateway.
type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Registry provides concurrency-safe tool discovery.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Definition)}
}

func (r *Registry) Register(definition Definition) error {
	if definition.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if definition.InputSchema == nil {
		return fmt.Errorf("tool %q input schema is required", definition.Name)
	}

	data, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("invalid tool schema: %w", err)
	}
	if err := json.Unmarshal(data, &definition); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool %q is already registered", definition.Name)
	}
	r.tools[definition.Name] = definition
	return nil
}

func (r *Registry) Lookup(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definition, ok := r.tools[name]
	return cloneDefinition(definition), ok
}

func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]Definition, 0, len(r.tools))
	for _, definition := range r.tools {
		definitions = append(definitions, cloneDefinition(definition))
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func cloneDefinition(definition Definition) Definition {
	data, _ := json.Marshal(definition)
	var result Definition
	json.Unmarshal(data, &result)
	return result
}
