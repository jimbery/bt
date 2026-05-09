package replay

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jayimbery/bt/pkg/model"
)

var headerFailureNameRE = regexp.MustCompile(`header "([^"]+)"`)

// FailureStillPresentAfterReplay reports whether a recorded failure would still
// fire given a new response (same rules as table assertions for status and headers).
func FailureStillPresentAfterReplay(f model.Failure, resp model.ResponseDetail) bool {
	switch f.Invariant {
	case model.InvariantStatusCode:
		want, ok := expectedAsInt(f.Expected)
		if !ok {
			return false
		}
		return resp.StatusCode != want
	case model.InvariantResponseHeader:
		name, ok := headerNameFromFailureMessage(f.Message)
		if !ok {
			return false
		}
		want, ok := expectedAsString(f.Expected)
		if !ok {
			return false
		}
		key := http.CanonicalHeaderKey(name)
		got := resp.Headers[key]
		return got != want
	default:
		return false
	}
}

func headerNameFromFailureMessage(msg string) (string, bool) {
	m := headerFailureNameRE.FindStringSubmatch(msg)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func expectedAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(x))
		return i, err == nil
	default:
		return 0, false
	}
}

func expectedAsString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatInt(int64(x), 10), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}
