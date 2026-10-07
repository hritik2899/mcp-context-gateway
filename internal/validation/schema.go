package validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type noExternalReferences struct{}

func (noExternalReferences) Load(string) (any, error) {
	return nil, errors.New("external schema references are disabled; use local $defs")
}
func Compile(schema map[string]any) (*jsonschema.Schema, error) {
	if schema == nil || schema["type"] != "object" {
		return nil, errors.New("tool inputSchema must have type object")
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(noExternalReferences{})
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("https://gateway.invalid/schema.json", doc); err != nil {
		return nil, err
	}
	compiled, err := compiler.Compile("https://gateway.invalid/schema.json")
	if err != nil {
		return nil, fmt.Errorf("invalid tool input schema: %w", err)
	}
	return compiled, nil
}
