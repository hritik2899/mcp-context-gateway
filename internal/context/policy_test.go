package contextpolicy

import (
	"encoding/json"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"strings"
	"testing"
)

func TestRedactsStructuredAndJSONText(t *testing.T) {
	var result mcp.CallToolResult
	json.Unmarshal([]byte(`{"content":[{"type":"text","text":"{\"token\":\"secret\",\"id\":9007199254740993}"}],"structuredContent":{"nested":{"PASSWORD":"secret"}},"isError":true}`), &result)
	got, err := Apply(result, config.ContextPolicy{RedactKeys: []string{"token", "password"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret") || !strings.Contains(string(encoded), "9007199254740993") || !got.IsError {
		t.Fatal(string(encoded))
	}
	original, _ := json.Marshal(result)
	if !strings.Contains(string(original), "secret") {
		t.Fatal("input result was mutated")
	}
}
func TestBudgetPreservesFailureAndValidJSON(t *testing.T) {
	result := mcp.TextResult(strings.Repeat("界\"\\\n", 1000), true)
	got, err := Apply(result, config.ContextPolicy{MaxOutputBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if len(encoded) > 512 || !got.IsError || !strings.Contains(string(encoded), "truncated") || !json.Valid(encoded) {
		t.Fatal(string(encoded))
	}
}
func TestDisabledPolicyPreservesResult(t *testing.T) {
	result := mcp.TextResult("complete", false)
	got, err := Apply(result, config.ContextPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(result)
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatal(string(after))
	}
}
