package config

import (
	"os"
	"strings"
)

// RequestHeaderOverrides returns HTTP headers derived from target.auth.
// Supported types: bearer (token from process environment named by auth.env).
func RequestHeaderOverrides(auth AuthConfig) map[string]string {
	t := strings.ToLower(strings.TrimSpace(auth.Type))
	env := strings.TrimSpace(auth.Env)
	if t == "" || env == "" {
		return nil
	}
	switch t {
	case "bearer":
		tok := strings.TrimSpace(os.Getenv(env))
		if tok == "" {
			return nil
		}
		if strings.HasPrefix(strings.ToLower(tok), "bearer ") {
			return map[string]string{"Authorization": tok}
		}
		return map[string]string{"Authorization": "Bearer " + tok}
	default:
		return nil
	}
}
