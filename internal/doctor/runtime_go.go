package doctor

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func client5s() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func goVersionString() string {
	return runtime.Version()
}

func goAtLeast121(version string) (bool, string) {
	// version like "go1.22.0" or "go1.21rc1"
	v := strings.TrimPrefix(version, "go")
	if v == "" {
		return false, "could not determine Go version"
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false, fmt.Sprintf("unexpected go version format: %s", version)
	}
	major, err1 := strconv.Atoi(parts[0])
	minorStr := parts[1]
	for i, r := range minorStr {
		if r < '0' || r > '9' {
			minorStr = minorStr[:i]
			break
		}
	}
	minor, err2 := strconv.Atoi(minorStr)
	if err1 != nil || err2 != nil {
		return false, fmt.Sprintf("could not parse Go version: %s", version)
	}
	if major > 1 || (major == 1 && minor >= 21) {
		return true, fmt.Sprintf("%s (>= go1.21)", version)
	}
	return false, fmt.Sprintf("%s is below go1.21", version)
}
