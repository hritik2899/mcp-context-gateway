package mcp

import (
	"encoding/json"
	"testing"
)

func TestNormalizeResultRejectsUnencodableOutput(t *testing.T) {
	if _, err := NormalizeResult(make(chan int)); err == nil {
		t.Fatal("expected serialization failure")
	}
}
func TestToolMetadataRoundTrip(t *testing.T) {
	raw := `{"name":"test","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true},"outputSchema":{"type":"object"}}`
	var tool ToolDefinition
	if err := json.Unmarshal([]byte(raw), &tool); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(encoded, &got)
	if got["annotations"] == nil || got["outputSchema"] == nil {
		t.Fatal(string(encoded))
	}
}
