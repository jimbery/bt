package openapi

import (
	"fmt"
	"strings"
)

func generateID(method, path string) string {
	method = strings.ToUpper(method)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.NewReplacer("{", "", "}", "").Replace(p)
		if clean != "" {
			segments = append(segments, clean)
		}
	}
	return method + "_" + strings.Join(segments, "_")
}

func parseStatusCode(code string) int {
	if code == "default" {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(code, "%d", &n)
	return n
}
