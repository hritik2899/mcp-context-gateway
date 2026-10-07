// Package contextpolicy applies explicit, per-request output policies. It never
// stores tool content, mixes callers' context, or makes external LLM calls.
package contextpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"github.com/hritik2899/mcp-context-gateway/internal/gateway"
	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"strings"
	"unicode/utf8"
)

type Service struct {
	Executor gateway.Executor
	Options  config.ContextPolicy
}

func (s *Service) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	value, err := s.Executor.Execute(ctx, name, args)
	if err != nil {
		return nil, err
	}
	if s.Options.MaxOutputBytes == 0 && s.Options.MaxEstimatedTokens == 0 && len(s.Options.RedactKeys) == 0 {
		return value, nil
	}
	result, err := mcp.NormalizeResult(value)
	if err != nil {
		return nil, err
	}
	return Apply(result, s.Options)
}
func decode(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	err := decoder.Decode(&value)
	return value, err
}
func redact(value any, keys map[string]bool) bool {
	changed := false
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if keys[strings.ToLower(key)] {
				v[key] = "[REDACTED]"
				changed = true
				continue
			}
			if key == "text" {
				if text, ok := child.(string); ok && json.Valid([]byte(text)) {
					parsed, err := decode([]byte(text))
					if err == nil && redact(parsed, keys) {
						encoded, _ := json.Marshal(parsed)
						v[key] = string(encoded)
						changed = true
						continue
					}
				}
			}
			changed = redact(child, keys) || changed
		}
	case []any:
		for _, child := range v {
			changed = redact(child, keys) || changed
		}
	}
	return changed
}
func Apply(result mcp.CallToolResult, options config.ContextPolicy) (mcp.CallToolResult, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	originalSize := len(raw)
	redacted := false
	if len(options.RedactKeys) > 0 {
		value, err := decode(raw)
		if err != nil {
			return result, err
		}
		keys := map[string]bool{}
		for _, key := range options.RedactKeys {
			keys[strings.ToLower(key)] = true
		}
		redacted = redact(value, keys)
		if redacted {
			raw, err = json.Marshal(value)
			if err != nil {
				return result, err
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				return result, err
			}
		}
	}
	budget := options.MaxOutputBytes
	if options.MaxEstimatedTokens > 0 {
		tokenBytes := options.MaxEstimatedTokens * 4
		if budget == 0 || tokenBytes < budget {
			budget = tokenBytes
		}
	}
	if budget == 0 || len(raw) <= budget {
		return result, nil
	}
	// Structured/binary content cannot be arbitrarily sliced without corrupting it.
	// Replace oversized output with an explicitly marked text excerpt, preserving isError.
	prefix := "[gateway: output truncated to configured context budget; structured and binary content omitted]\n"
	preview := ""
	for _, block := range result.Content {
		if block.Type == "text" {
			preview += block.Text + "\n"
			if len(preview) > budget {
				break
			}
		}
	}
	truncated := mcp.TextResult(prefix, result.IsError)
	truncated.Meta = map[string]any{"gateway": map[string]any{"truncated": true, "originalBytes": originalSize, "redacted": redacted}}
	encoded, err := json.Marshal(truncated)
	if err != nil {
		return result, err
	}
	if len(encoded) > budget {
		return result, fmt.Errorf("context budget too small for truncation notice")
	}
	// Binary search on UTF-8 byte boundaries; count serialized JSON, including escaping.
	low, high := 0, len(preview)
	for low < high {
		mid := (low + high + 1) / 2
		end := mid
		for end > 0 && !utf8.ValidString(preview[:end]) {
			end--
		}
		truncated.Content[0].Text = prefix + preview[:end]
		encoded, _ = json.Marshal(truncated)
		if len(encoded) <= budget {
			low = mid
		} else {
			high = mid - 1
		}
	}
	for low > 0 && !utf8.ValidString(preview[:low]) {
		low--
	}
	truncated.Content[0].Text = prefix + preview[:low]
	return truncated, nil
}
