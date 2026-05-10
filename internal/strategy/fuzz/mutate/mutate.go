// Package mutate implements deterministic request mutations for fuzz testing.
package mutate

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"unicode"

	"github.com/jayimbery/bt/pkg/model"
)

// Input carries mutable parts of an HTTP request.
type Input struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`

	GQLQuery     string         `json:"gql_query,omitempty"`
	GQLVariables map[string]any `json:"gql_variables,omitempty"`
}

// Mutator transforms a seed input using randomness from r.
type Mutator interface {
	Name() string
	Mutate(seed Input, r *rand.Rand) Input
}

// --- cloning ---

func cloneInput(seed Input) Input {
	out := Input{
		Method: seed.Method,
		Path:   seed.Path,
	}
	if seed.Query != nil {
		out.Query = make(map[string]string, len(seed.Query))
		for k, v := range seed.Query {
			out.Query[k] = v
		}
	}
	if seed.Headers != nil {
		out.Headers = make(map[string]string, len(seed.Headers))
		for k, v := range seed.Headers {
			out.Headers[k] = v
		}
	}
	if len(seed.Body) > 0 {
		out.Body = append([]byte(nil), seed.Body...)
	}
	out.GQLQuery = seed.GQLQuery
	if seed.GQLVariables != nil {
		out.GQLVariables = cloneAnyMap(seed.GQLVariables)
	}
	return out
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// --- PayloadMutator ---

type payloadMutator struct{}

// NewPayloadMutator returns a mutator for JSON bodies and related headers.
func NewPayloadMutator() Mutator { return payloadMutator{} }

func (payloadMutator) Name() string { return "payload" }

func (payloadMutator) Mutate(seed Input, r *rand.Rand) Input {
	if strings.TrimSpace(seed.GQLQuery) != "" {
		in := cloneInput(seed)
		q := in.GQLQuery
		switch r.Intn(6) {
		case 0, 1:
			if len(q) > 2 {
				cut := 1 + r.Intn(len(q)-1)
				in.GQLQuery = q[:cut]
			}
		case 2:
			in.GQLQuery = q + "}"
		case 3:
			in.GQLQuery = ""
		case 4:
			if in.GQLVariables == nil {
				in.GQLVariables = map[string]any{}
			} else {
				in.GQLVariables = cloneAnyMap(in.GQLVariables)
			}
			in.GQLVariables["_fuzz"] = strings.Repeat("x", 128)
		default:
			if len(q) > 0 {
				pos := r.Intn(len(q))
				b := byte('a' + r.Intn(26))
				if pos+1 >= len(q) {
					in.GQLQuery = q[:pos] + string(b)
				} else {
					in.GQLQuery = q[:pos] + string(b) + q[pos+1:]
				}
			}
		}
		return in
	}

	in := cloneInput(seed)
	if len(in.Body) == 0 {
		return in
	}
	// Bias toward truncate / strip so doc's statistical tests hit within ~50 seeds.
	switch r.Intn(10) {
	case 0, 1, 2, 3:
		if n := len(in.Body); n > 1 {
			cut := 1 + r.Intn(n-1)
			in.Body = in.Body[:cut]
		}
	case 4:
		pos := r.Intn(len(in.Body) + 1)
		in.Body = append(in.Body[:pos], append([]byte{0}, in.Body[pos:]...)...)
	case 5:
		pos := r.Intn(len(in.Body))
		in.Body[pos] ^= byte(1 << (r.Intn(8)))
	case 6:
		in.Body = []byte(replaceRandomJSONStringValue(string(in.Body), r))
	case 7:
		in.Body = []byte(duplicateRandomJSONKey(string(in.Body), r))
	case 8:
		if in.Headers == nil {
			in.Headers = map[string]string{}
		}
		delete(in.Headers, "Content-Type")
	default:
		if len(in.Body) > 2 {
			in.Body = in.Body[:len(in.Body)-1-r.Intn(2)]
		}
	}
	return in
}

func replaceRandomJSONStringValue(s string, r *rand.Rand) string {
	inners := []string{
		"",
		"X",
		"null",
		"true",
		"false",
		"0",
		"9999999",
		strings.Repeat("A", 10000),
		strings.Repeat(" ", 200),
		`\u0000`,
		"\U0001F600",
	}
	for i := 0; i < len(s)-2; i++ {
		if s[i] != ':' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j >= len(s) || s[j] != '"' {
			continue
		}
		open := j
		end := open + 1
		for end < len(s) {
			if s[end] == '\\' {
				if end+1 < len(s) {
					end += 2
					continue
				}
				break
			}
			if s[end] == '"' {
				innerPick := inners[r.Intn(len(inners))]
				if innerPick == "null" || innerPick == "true" || innerPick == "false" || innerPick == "0" || innerPick == "9999999" {
					// Replace quoted JSON value with a literal token.
					return s[:open] + innerPick + s[end+1:]
				}
				return s[:open+1] + innerPick + `"` + s[end+1:]
			}
			end++
		}
	}
	return s
}

func duplicateRandomJSONKey(s string, r *rand.Rand) string {
	_ = r
	i := strings.Index(s, `"`)
	if i < 0 {
		return s + `,"_dup":null`
	}
	j := i + 1
	for j < len(s) && s[j] != '"' {
		if s[j] == '\\' {
			j += 2
			continue
		}
		j++
	}
	if j >= len(s) || j+1 >= len(s) || s[j+1] != ':' {
		return s + `,"_dup":null`
	}
	keyQuoted := s[i : j+1]
	pos := j + 2
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t') {
		pos++
	}
	if pos >= len(s) {
		return s + `,"_dup":null`
	}
	valEnd := pos
	if s[pos] == '"' {
		valEnd = pos + 1
		for valEnd < len(s) {
			if s[valEnd] == '\\' {
				if valEnd+1 < len(s) {
					valEnd += 2
					continue
				}
				break
			}
			if s[valEnd] == '"' {
				valEnd++
				break
			}
			valEnd++
		}
	} else {
		for valEnd < len(s) && (unicode.IsDigit(rune(s[valEnd])) || s[valEnd] == '.' || s[valEnd] == 'e' || s[valEnd] == 'E' || s[valEnd] == '-' || s[valEnd] == '+') {
			valEnd++
		}
	}
	dup := "," + keyQuoted + ":" + s[pos:valEnd]
	return s[:valEnd] + dup + s[valEnd:]
}

// --- HeaderMutator ---

type headerMutator struct{}

func NewHeaderMutator() Mutator { return headerMutator{} }

func (headerMutator) Name() string { return "header" }

func (headerMutator) Mutate(seed Input, r *rand.Rand) Input {
	in := cloneInput(seed)
	if in.Headers == nil {
		in.Headers = map[string]string{}
	}
	keys := make([]string, 0, len(in.Headers))
	for k := range in.Headers {
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		switch r.Intn(5) {
		case 0:
			k := keys[r.Intn(len(keys))]
			delete(in.Headers, k)
		case 1:
			k := keys[r.Intn(len(keys))]
			in.Headers[k] = ""
		case 2:
			k := keys[r.Intn(len(keys))]
			in.Headers[k] = strings.Repeat("H", 8192)
		case 3:
			// Splice extra tokens into the value. Must not insert CR/LF — net/http rejects
			// those before the request is sent, which turns fuzz into client-side "crash"
			// noise instead of exercising the server.
			k := keys[r.Intn(len(keys))]
			v := in.Headers[k]
			if len(v) == 0 {
				v = "x"
			}
			pos := r.Intn(len(v) + 1)
			in.Headers[k] = v[:pos] + "\t;fuzz=" + strings.Repeat("z", r.Intn(32)+1) + v[pos:]
		case 4:
			k := keys[r.Intn(len(keys))]
			in.Headers[k] = in.Headers[k] + "fuzz"
		}
	}
	in.Headers["X-Fuzz-Unknown"] = "fuzz"
	return in
}

// --- PathMutator ---

type pathMutator struct{}

func NewPathMutator() Mutator { return pathMutator{} }

func (pathMutator) Name() string { return "path" }

func (pathMutator) Mutate(seed Input, r *rand.Rand) Input {
	in := cloneInput(seed)
	p := in.Path
	trim := strings.Trim(p, "/")
	if trim == "" {
		return in
	}
	segs := strings.Split(trim, "/")
	if len(segs) == 0 {
		return in
	}
	si := r.Intn(len(segs))
	exts := []string{".json", ".php", ".xml", ".env"}
	special := []string{"%00", "<script>", "' OR 1=1--"}
	switch r.Intn(6) {
	case 0:
		// Never clear the only URL segment: /health -> / hits a different route and
		// produces statuses not declared on this operation (e.g. GET / vs GET /health).
		if len(segs) == 1 {
			segs[si] = segs[si] + "_"
		} else {
			segs[si] = ""
		}
	case 1:
		segs[si] = strings.Repeat("P", 2048)
	case 2:
		// Avoid "../.." on a single path segment — it normalizes to "/" and leaves
		// the operation's declared surface (spurious unexpected_status / noise).
		if len(segs) == 1 {
			segs[si] = segs[si] + "_seg"
		} else {
			segs[si] = "../.."
		}
	case 3:
		segs[si] = special[r.Intn(len(special))]
	case 4:
		in.Path = "/" + strings.Join(segs, "/") + exts[r.Intn(len(exts))]
		return in
	default:
		segs[si] = segs[si] + "x"
	}
	in.Path = "/" + strings.Join(segs, "/")
	return in
}

// --- QueryMutator ---

type queryMutator struct{}

func NewQueryMutator() Mutator { return queryMutator{} }

func (queryMutator) Name() string { return "query" }

func (queryMutator) Mutate(seed Input, r *rand.Rand) Input {
	in := cloneInput(seed)
	if in.Query == nil {
		in.Query = map[string]string{}
	}
	if len(in.Query) == 0 {
		in.Query["fuzz"] = "1"
		return in
	}
	keys := make([]string, 0, len(in.Query))
	for k := range in.Query {
		keys = append(keys, k)
	}
	k := keys[r.Intn(len(keys))]
	switch r.Intn(5) {
	case 0:
		delete(in.Query, k)
	case 1:
		in.Query[k] = ""
	case 2:
		in.Query[k] = strings.Repeat("Q", 4096)
	case 3:
		opts := []string{"' OR 1=1--", "<script>alert(1)</script>", "%00"}
		in.Query[k] = opts[r.Intn(len(opts))]
	default:
		in.Query[k+"_dup"] = in.Query[k]
	}
	return in
}

// --- MutatorSet ---

// MutatorSet runs several mutators in registration order.
type MutatorSet struct {
	ms []Mutator
}

// NewMutatorSet returns a set containing the given mutators.
func NewMutatorSet(ms ...Mutator) *MutatorSet {
	cp := append([]Mutator(nil), ms...)
	return &MutatorSet{ms: cp}
}

// MutateAll applies each mutator once to a clone of seed and returns the variants.
func (s *MutatorSet) MutateAll(seed Input, r *rand.Rand) []Input {
	out := make([]Input, 0, len(s.ms))
	for _, m := range s.ms {
		out = append(out, m.Mutate(cloneInput(seed), r))
	}
	return out
}

// CaseInputFrom converts Input into model.CaseInput for the HTTP runner.
func CaseInputFrom(in Input) model.CaseInput {
	if strings.TrimSpace(in.GQLQuery) != "" {
		ci := model.CaseInput{
			Method:       in.Method,
			Path:         in.Path,
			GQLQuery:     strings.TrimSpace(in.GQLQuery),
			GQLVariables: cloneAnyMap(in.GQLVariables),
		}
		if len(in.Query) > 0 {
			ci.Query = make(map[string]string, len(in.Query))
			for k, v := range in.Query {
				ci.Query[k] = v
			}
		}
		if len(in.Headers) > 0 {
			ci.Headers = make(map[string]string, len(in.Headers))
			for k, v := range in.Headers {
				ci.Headers[k] = v
			}
		}
		return ci
	}

	ci := model.CaseInput{
		Method: in.Method,
		Path:   in.Path,
	}
	if len(in.Query) > 0 {
		ci.Query = make(map[string]string, len(in.Query))
		for k, v := range in.Query {
			ci.Query[k] = v
		}
	}
	if len(in.Headers) > 0 {
		ci.Headers = make(map[string]string, len(in.Headers))
		for k, v := range in.Headers {
			ci.Headers[k] = v
		}
	}
	if len(in.Body) == 0 {
		return ci
	}
	var body any
	if err := json.Unmarshal(in.Body, &body); err != nil {
		ci.Body = json.RawMessage(append([]byte(nil), in.Body...))
	} else {
		ci.Body = body
	}
	return ci
}

// FromCaseInput builds mutate.Input from a case (best-effort).
func InputFromCaseInput(method, path string, query, headers map[string]string, body any) Input {
	in := Input{
		Method:  method,
		Path:    path,
		Query:   query,
		Headers: headers,
	}
	if body == nil {
		return in
	}
	b, err := json.Marshal(body)
	if err == nil {
		in.Body = b
	}
	return in
}

// MarshalJSON implements custom JSON for stable corpus hashing.
func (in Input) MarshalJSON() ([]byte, error) {
	type aux struct {
		Method       string            `json:"method"`
		Path         string            `json:"path"`
		Query        map[string]string `json:"query"`
		Headers      map[string]string `json:"headers"`
		Body         any               `json:"body"`
		GQLQuery     string            `json:"gql_query,omitempty"`
		GQLVariables map[string]any    `json:"gql_variables,omitempty"`
	}
	a := aux{
		Method:       in.Method,
		Path:         in.Path,
		Query:        in.Query,
		Headers:      in.Headers,
		GQLQuery:     in.GQLQuery,
		GQLVariables: in.GQLVariables,
	}
	switch {
	case len(in.Body) == 0:
		a.Body = nil
	case json.Valid(in.Body):
		a.Body = json.RawMessage(in.Body)
	default:
		// Truncated / corrupted JSON fragments are not valid RawMessage for encoding/json.
		a.Body = string(in.Body)
	}
	return json.Marshal(a)
}

// UnmarshalJSON loads Input from corpus JSON.
func (in *Input) UnmarshalJSON(data []byte) error {
	type aux struct {
		Method       string            `json:"method"`
		Path         string            `json:"path"`
		Query        map[string]string `json:"query"`
		Headers      map[string]string `json:"headers"`
		Body         json.RawMessage   `json:"body"`
		GQLQuery     string            `json:"gql_query"`
		GQLVariables map[string]any    `json:"gql_variables"`
	}
	var a aux
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	in.Method = a.Method
	in.Path = a.Path
	in.Query = a.Query
	in.Headers = a.Headers
	in.GQLQuery = a.GQLQuery
	in.GQLVariables = a.GQLVariables
	if len(a.Body) == 0 || string(a.Body) == "null" {
		return nil
	}
	// Body may be a JSON string (mutated non-JSON fragment) or a JSON object/array.
	var s string
	if err := json.Unmarshal(a.Body, &s); err == nil {
		in.Body = []byte(s)
		return nil
	}
	in.Body = append([]byte(nil), a.Body...)
	return nil
}

// Equal compares two inputs for tests (not used in prod hot path).
func Equal(a, b Input) bool {
	return a.Method == b.Method && a.Path == b.Path &&
		mapsEqual(a.Query, b.Query) && mapsEqual(a.Headers, b.Headers) &&
		bytes.Equal(a.Body, b.Body) && a.GQLQuery == b.GQLQuery &&
		reflect.DeepEqual(a.GQLVariables, b.GQLVariables)
}

// Clone returns a deep copy of in.
func Clone(in Input) Input {
	return cloneInput(in)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
