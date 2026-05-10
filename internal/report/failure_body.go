package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const maxFailureBodyPrintBytes = 16384

// FailureBodyForDisplay returns a printable response (or request) body for failure output.
// JSON is pretty-printed when valid; UTF-8 text is shown as-is; binary is shown as hex prefix.
func FailureBodyForDisplay(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	trim := b
	truncated := false
	if len(trim) > maxFailureBodyPrintBytes {
		trim = trim[:maxFailureBodyPrintBytes]
		truncated = true
	}
	if json.Valid(trim) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, trim, "", "  "); err == nil {
			s := buf.String()
			if truncated {
				return s + fmt.Sprintf("\n… truncated for display (%d bytes total)", len(b))
			}
			return s
		}
	}
	if utf8.Valid(trim) {
		s := string(trim)
		if truncated {
			return s + fmt.Sprintf("\n… truncated for display (%d bytes total)", len(b))
		}
		return s
	}
	prefix := trim
	if len(prefix) > 128 {
		prefix = prefix[:128]
	}
	return fmt.Sprintf("(binary, %d bytes)\n%x", len(b), prefix)
}
