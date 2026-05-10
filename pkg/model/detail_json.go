package model

import (
	"encoding/base64"
	"encoding/json"
	"unicode/utf8"
)

// marshalHTTPBodyForJSON encodes a request or response body for artifact/report JSON:
// valid JSON is embedded as a JSON object/array; UTF-8 text as a string; otherwise base64.
func marshalHTTPBodyForJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	if json.Valid(b) {
		return json.RawMessage(append([]byte(nil), b...))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func unmarshalHTTPBodyJSON(msg json.RawMessage) ([]byte, error) {
	if len(msg) == 0 || string(msg) == "null" {
		return nil, nil
	}
	switch msg[0] {
	case '{', '[':
		if !json.Valid(msg) {
			return append([]byte(nil), msg...), nil
		}
		return append([]byte(nil), msg...), nil
	default:
		var s string
		if err := json.Unmarshal(msg, &s); err != nil {
			return nil, err
		}
		if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
			return dec, nil
		}
		return []byte(s), nil
	}
}

// MarshalJSON writes human-readable JSON for artifacts and reports (body as object or string, not base64).
func (r RequestDetail) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"method": r.Method,
		"url":    r.URL,
	}
	if len(r.Headers) > 0 {
		m["headers"] = r.Headers
	}
	if len(r.Query) > 0 {
		m["query"] = r.Query
	}
	if len(r.Body) > 0 {
		m["body"] = marshalHTTPBodyForJSON(r.Body)
	}
	return json.Marshal(m)
}

// UnmarshalJSON loads RequestDetail from JSON (legacy base64 string bodies still decode).
func (r *RequestDetail) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["method"]; ok {
		_ = json.Unmarshal(v, &r.Method)
	}
	if v, ok := raw["url"]; ok {
		_ = json.Unmarshal(v, &r.URL)
	}
	if v, ok := raw["headers"]; ok {
		_ = json.Unmarshal(v, &r.Headers)
	}
	if v, ok := raw["query"]; ok {
		_ = json.Unmarshal(v, &r.Query)
	}
	if v, ok := raw["body"]; ok {
		b, err := unmarshalHTTPBodyJSON(v)
		if err != nil {
			return err
		}
		r.Body = b
	}
	return nil
}

// MarshalJSON writes human-readable JSON for artifacts and reports.
func (r ResponseDetail) MarshalJSON() ([]byte, error) {
	m := map[string]any{"status_code": r.StatusCode}
	if len(r.Headers) > 0 {
		m["headers"] = r.Headers
	}
	if len(r.Body) > 0 {
		m["body"] = marshalHTTPBodyForJSON(r.Body)
	}
	return json.Marshal(m)
}

// UnmarshalJSON loads ResponseDetail from JSON (legacy base64 string bodies still decode).
func (r *ResponseDetail) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["status_code"]; ok {
		_ = json.Unmarshal(v, &r.StatusCode)
	}
	if v, ok := raw["headers"]; ok {
		_ = json.Unmarshal(v, &r.Headers)
	}
	if v, ok := raw["body"]; ok {
		b, err := unmarshalHTTPBodyJSON(v)
		if err != nil {
			return err
		}
		r.Body = b
	}
	return nil
}
