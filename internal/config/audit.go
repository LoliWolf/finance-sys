package config

import (
	"encoding/json"
	"strings"
)

// AuditJSON is for persistence/diagnostics only, never a runtime configuration.
// Keep the original configuration exclusively in runtime/Nacos credentials.
func AuditJSON(raw []byte) []byte {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return []byte(`{"error":"invalid configuration; omitted"}`)
	}
	var redact func(any)
	redact = func(v any) {
		switch item := v.(type) {
		case map[string]any:
			for key, child := range item {
				lower := strings.ToLower(key)
				if lower == "dsn" || lower == "token" || lower == "tokens" || lower == "static_tokens" || lower == "internal_token" || lower == "api_key" || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
					item[key] = "<redacted>"
				} else {
					redact(child)
				}
			}
		case []any:
			for _, child := range item {
				redact(child)
			}
		}
	}
	redact(value)
	result, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return result
}
