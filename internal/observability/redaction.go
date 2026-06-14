package observability

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxStringValueLen = 512

var forbiddenKeyParts = []string{
	"private",
	"secret",
	"token",
	"password",
	"invitecode",
	"invite_code",
	"auth",
	"bearer",
	"key",
}

func sanitizeFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if forbiddenKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = sanitizeValue(key, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeValue(key string, value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return sanitizeString(key, typed)
	case fmt.Stringer:
		return sanitizeString(key, typed.String())
	case error:
		return sanitizeString(key, typed.Error())
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeString(key, item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(key, item))
		}
		return out
	case map[string]any:
		return sanitizeFields(typed)
	default:
		return typed
	}
}

func sanitizeString(key, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if looksPathKey(key) {
		value = filepath.Base(value)
	}
	if len(value) > maxStringValueLen {
		return value[:maxStringValueLen] + "...[truncated]"
	}
	return value
}

func forbiddenKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), ".", "_"))
	for _, part := range forbiddenKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func looksPathKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(normalized, "path") || strings.Contains(normalized, "filename")
}
