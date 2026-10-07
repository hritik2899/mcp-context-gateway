package mcp

import (
	"bytes"
	"encoding/json"
)

// ToolDefinition retains upstream metadata (annotations, outputSchema, etc.).
type ToolDefinition struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	InputSchema map[string]any             `json:"inputSchema"`
	Extra       map[string]json.RawMessage `json:"-"`
}

func (t *ToolDefinition) UnmarshalJSON(data []byte) error {
	type plain ToolDefinition
	var p plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&p); err != nil {
		return err
	}
	*t = ToolDefinition(p)
	if err := json.Unmarshal(data, &t.Extra); err != nil {
		return err
	}
	for _, key := range []string{"name", "description", "inputSchema"} {
		delete(t.Extra, key)
	}
	return nil
}
func (t ToolDefinition) MarshalJSON() ([]byte, error) {
	result := make(map[string]json.RawMessage, len(t.Extra)+3)
	for k, v := range t.Extra {
		result[k] = v
	}
	result["name"], _ = json.Marshal(t.Name)
	var err error
	result["inputSchema"], err = json.Marshal(t.InputSchema)
	if err != nil {
		return nil, err
	}
	if t.Description != "" {
		result["description"], _ = json.Marshal(t.Description)
	}
	return json.Marshal(result)
}

type ToolsListResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

const ToolsListMethod = "tools/list"
