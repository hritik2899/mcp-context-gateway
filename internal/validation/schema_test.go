package validation

import (
	"testing"
)

func TestExternalReferencesBlockedAndLocalDefinitionsWork(t *testing.T) {
	if _, err := Compile(map[string]any{"type": "object", "$ref": "file:///etc/passwd"}); err == nil {
		t.Fatal("external references allowed")
	}
	schema, err := Compile(map[string]any{"type": "object", "$defs": map[string]any{"name": map[string]any{"type": "string"}}, "properties": map[string]any{"name": map[string]any{"$ref": "#/$defs/name"}}})
	if err != nil {
		t.Fatal(err)
	}
	if schema.Validate(map[string]any{"name": 42}) == nil {
		t.Fatal("invalid argument accepted")
	}
}
