// Package har parses HTTP Archive (HAR) 1.1/1.2 files for trace analysis.
package har

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

var (
	ErrHARMalformed          = errors.New("har: malformed json")
	ErrHARVersionUnsupported = errors.New("har: unsupported log.version")
)

func IsErrHARMalformed(err error) bool { return errors.Is(err, ErrHARMalformed) }
func IsErrHARVersionUnsupported(err error) bool {
	return errors.Is(err, ErrHARVersionUnsupported)
}

// HAR is the root HAR document.
type HAR struct {
	Log Log `json:"log"`
}

// Log is the HAR log object.
type Log struct {
	Version string     `json:"version"`
	Entries []HAREntry `json:"entries"`
}

// HAREntry is a normalised HAR log entry.
type HAREntry struct {
	URL             string
	Method          string
	RequestBody     []byte
	ResponseStatus  int
	ResponseBody    []byte
	StartedDateTime time.Time
	TimingMs        float64
	ServerIPAddress string
}

// ToEntries returns a copy of log entries (already normalised at parse time).
func (h *HAR) ToEntries() []HAREntry {
	if h == nil {
		return nil
	}
	out := make([]HAREntry, len(h.Log.Entries))
	copy(out, h.Log.Entries)
	return out
}

type rawHAR struct {
	Log rawLog `json:"log"`
}

type rawLog struct {
	Version string     `json:"version"`
	Entries []rawEntry `json:"entries"`
}

type rawEntry struct {
	StartedDateTime string  `json:"startedDateTime"`
	Time            float64 `json:"time"`
	Request         rawReq  `json:"request"`
	Response        rawResp `json:"response"`
	ServerIPAddress string  `json:"serverIPAddress"`
}

type rawReq struct {
	Method   string    `json:"method"`
	URL      string    `json:"url"`
	PostData *postData `json:"postData"`
}

type postData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type rawResp struct {
	Status  int         `json:"status"`
	Content *rawContent `json:"content"`
}

type rawContent struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// Parse parses HAR 1.1/1.2 JSON.
func Parse(r io.Reader) (*HAR, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var root rawHAR
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHARMalformed, err)
	}
	v := strings.TrimSpace(root.Log.Version)
	if v != "1.1" && v != "1.2" {
		return nil, fmt.Errorf("%w: %q", ErrHARVersionUnsupported, v)
	}
	out := &HAR{Log: Log{Version: v, Entries: make([]HAREntry, 0, len(root.Log.Entries))}}
	for _, e := range root.Log.Entries {
		ent, err := normaliseEntry(e)
		if err != nil {
			return nil, err
		}
		out.Log.Entries = append(out.Log.Entries, ent)
	}
	return out, nil
}

func normaliseEntry(e rawEntry) (HAREntry, error) {
	var t time.Time
	if strings.TrimSpace(e.StartedDateTime) != "" {
		var err error
		t, err = time.Parse(time.RFC3339Nano, e.StartedDateTime)
		if err != nil {
			t, err = time.Parse(time.RFC3339, e.StartedDateTime)
			if err != nil {
				return HAREntry{}, fmt.Errorf("%w: startedDateTime: %v", ErrHARMalformed, err)
			}
		}
	}
	method := strings.ToUpper(strings.TrimSpace(e.Request.Method))
	u := strings.TrimSpace(e.Request.URL)

	var reqBody []byte
	if e.Request.PostData != nil && jsonMime(e.Request.PostData.MimeType) {
		reqBody = []byte(e.Request.PostData.Text)
	}

	status := e.Response.Status
	var respBody []byte
	if e.Response.Content != nil && jsonMime(e.Response.Content.MimeType) && e.Response.Content.Text != "" {
		respBody = []byte(e.Response.Content.Text)
	}
	// Missing network response: Chrome may emit status 0 with empty content.
	if status == 0 && (e.Response.Content == nil || e.Response.Content.Text == "") {
		respBody = nil
	}

	return HAREntry{
		URL:             u,
		Method:          method,
		RequestBody:     reqBody,
		ResponseStatus:  status,
		ResponseBody:    respBody,
		StartedDateTime: t,
		TimingMs:        e.Time,
		ServerIPAddress: strings.TrimSpace(e.ServerIPAddress),
	}, nil
}

func jsonMime(m string) bool {
	m = strings.ToLower(strings.TrimSpace(m))
	return strings.Contains(m, "json") || m == "text/plain" || m == ""
}

// Filter returns entries whose URL host matches host (case-insensitive); port ignored.
// Empty host returns all entries.
func Filter(entries []HAREntry, host string) []HAREntry {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		out := make([]HAREntry, len(entries))
		copy(out, entries)
		return out
	}
	var out []HAREntry
	for _, e := range entries {
		h := entryHost(e.URL)
		if strings.EqualFold(h, host) {
			out = append(out, e)
		}
	}
	return out
}

func entryHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	h := u.Hostname()
	return strings.ToLower(h)
}
