package mutate_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/strategy/fuzz/mutate"
)

// --- Helpers ---

func rng(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func validJSONInput() mutate.Input {
	return mutate.Input{
		Method: "POST",
		Path:   "/orders/ord-001",
		Query:  map[string]string{"status": "pending"},
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer token",
		},
		Body: []byte(`{"amount":99.99,"currency":"GBP","status":"pending"}`),
	}
}

func emptyInput() mutate.Input {
	return mutate.Input{
		Method:  "GET",
		Path:    "/",
		Query:   map[string]string{},
		Headers: map[string]string{},
		Body:    []byte{},
	}
}

// --- PayloadMutator ---

func TestPayloadMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewPayloadMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestPayloadMutator_Mutate_ReturnsInput(t *testing.T) {
	m := mutate.NewPayloadMutator()
	result := m.Mutate(validJSONInput(), rng(1))
	// Must return a valid Input struct, not panic.
	_ = result
}

func TestPayloadMutator_Mutate_DoesNotPanicOnEmptyBody(t *testing.T) {
	m := mutate.NewPayloadMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PayloadMutator panicked on empty body: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(2))
}

func TestPayloadMutator_Mutate_DoesNotPanicOnNilBody(t *testing.T) {
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	input.Body = nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PayloadMutator panicked on nil body: %v", r)
		}
	}()
	m.Mutate(input, rng(3))
}

func TestPayloadMutator_Mutate_ProducesChangedBody_OverManyRuns(t *testing.T) {
	// Over 20 seeds, at least one must produce a body different from the original.
	m := mutate.NewPayloadMutator()
	original := validJSONInput()
	changed := false
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(original, rng(seed))
		if string(result.Body) != string(original.Body) {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("PayloadMutator never changed the body over 20 seeds")
	}
}

func TestPayloadMutator_Mutate_IsDeterministic(t *testing.T) {
	// Same rand seed must produce same result.
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	r1 := m.Mutate(input, rng(42))
	r2 := m.Mutate(input, rng(42))
	if string(r1.Body) != string(r2.Body) {
		t.Error("PayloadMutator must be deterministic for the same rand seed")
	}
}

func TestPayloadMutator_Truncate_ProducesShorterBody(t *testing.T) {
	// Run enough seeds to hit the truncate mutation.
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	truncated := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if len(result.Body) < len(input.Body) {
			truncated = true
			break
		}
	}
	if !truncated {
		t.Error("PayloadMutator never produced a truncated body over 50 seeds")
	}
}

func TestPayloadMutator_StripContentType_RemovesHeader(t *testing.T) {
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	stripped := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if _, present := result.Headers["Content-Type"]; !present {
			stripped = true
			break
		}
	}
	if !stripped {
		t.Error("PayloadMutator never stripped Content-Type over 50 seeds")
	}
}

// --- HeaderMutator ---

func TestHeaderMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewHeaderMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestHeaderMutator_Mutate_DoesNotPanicOnEmptyHeaders(t *testing.T) {
	m := mutate.NewHeaderMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HeaderMutator panicked on empty headers: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(1))
}

func TestHeaderMutator_Mutate_EmptyHeaders_AddsUnknownHeader(t *testing.T) {
	m := mutate.NewHeaderMutator()
	result := m.Mutate(emptyInput(), rng(1))
	if _, ok := result.Headers["X-Fuzz-Unknown"]; !ok {
		t.Error("HeaderMutator must add X-Fuzz-Unknown header when input headers are empty")
	}
}

func TestHeaderMutator_Mutate_AlwaysAddsUnknownHeader(t *testing.T) {
	m := mutate.NewHeaderMutator()
	// X-Fuzz-Unknown should appear in every mutated output.
	input := validJSONInput()
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(input, rng(seed))
		if _, ok := result.Headers["X-Fuzz-Unknown"]; !ok {
			t.Errorf("HeaderMutator missing X-Fuzz-Unknown for seed %d", seed)
		}
	}
}

func TestHeaderMutator_OversizedValue_Produces8192CharValue(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Headers {
			if len(v) == 8192 {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("HeaderMutator never produced an 8192-character header value over 50 seeds")
	}
}

func TestHeaderMutator_InjectNewline_ProducesNewlineInValue(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Headers {
			if strings.Contains(v, "\r\n") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("HeaderMutator never injected \\r\\n into a header value over 50 seeds")
	}
}

func TestHeaderMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	originalHeaderCount := len(input.Headers)
	m.Mutate(input, rng(1))
	// Original input must not be modified.
	if len(input.Headers) != originalHeaderCount {
		t.Error("HeaderMutator must not modify the original input's headers map")
	}
}

// --- PathMutator ---

func TestPathMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewPathMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestPathMutator_Mutate_DoesNotPanicOnRootPath(t *testing.T) {
	m := mutate.NewPathMutator()
	input := emptyInput() // path is "/"
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PathMutator panicked on root path: %v", r)
		}
	}()
	m.Mutate(input, rng(1))
}

func TestPathMutator_Mutate_ProducesChangedPath_OverManyRuns(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	changed := false
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(input, rng(seed))
		if result.Path != input.Path {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("PathMutator never changed the path over 20 seeds")
	}
}

func TestPathMutator_InjectTraversal_ProducesDoubleDot(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if strings.Contains(result.Path, "..") {
			found = true
			break
		}
	}
	if !found {
		t.Error("PathMutator never injected a traversal sequence over 50 seeds")
	}
}

func TestPathMutator_AppendExtension_AppendsToPath(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	found := false
	knownExtensions := []string{".json", ".php", ".xml", ".env"}
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, ext := range knownExtensions {
			if strings.HasSuffix(result.Path, ext) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("PathMutator never appended a file extension over 50 seeds")
	}
}

func TestPathMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	original := input.Path
	m.Mutate(input, rng(1))
	if input.Path != original {
		t.Error("PathMutator must not modify the original input's Path field")
	}
}

// --- QueryMutator ---

func TestQueryMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewQueryMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestQueryMutator_Mutate_DoesNotPanicOnEmptyQuery(t *testing.T) {
	m := mutate.NewQueryMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("QueryMutator panicked on empty query: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(1))
}

func TestQueryMutator_Mutate_EmptyQuery_AddsFuzzParam(t *testing.T) {
	m := mutate.NewQueryMutator()
	result := m.Mutate(emptyInput(), rng(1))
	if _, ok := result.Query["fuzz"]; !ok {
		t.Error("QueryMutator must add 'fuzz' param when input query is empty")
	}
}

func TestQueryMutator_InjectSpecialChars_ProducesSQLInjection(t *testing.T) {
	m := mutate.NewQueryMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Query {
			if strings.Contains(v, "OR 1=1") || strings.Contains(v, "<script>") || strings.Contains(v, "%00") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("QueryMutator never produced a special-character query value over 50 seeds")
	}
}

func TestQueryMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewQueryMutator()
	input := validJSONInput()
	original := map[string]string{}
	for k, v := range input.Query {
		original[k] = v
	}
	m.Mutate(input, rng(1))
	for k, v := range original {
		if input.Query[k] != v {
			t.Errorf("QueryMutator modified original query key %q", k)
		}
	}
}

// --- MutatorSet ---

func TestMutatorSet_MutateAll_ReturnsOneResultPerMutator(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
		mutate.NewPathMutator(),
		mutate.NewQueryMutator(),
	)
	results := set.MutateAll(validJSONInput(), rng(1))
	if len(results) != 4 {
		t.Errorf("expected 4 results (one per mutator), got %d", len(results))
	}
}

func TestMutatorSet_MutateAll_ResultOrderMatchesMutatorOrder(t *testing.T) {
	// Payload is first, header second. We can identify them by checking which field changed.
	payload := mutate.NewPayloadMutator()
	header := mutate.NewHeaderMutator()
	set := mutate.NewMutatorSet(payload, header)
	input := validJSONInput()
	results := set.MutateAll(input, rng(99))

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The second result (header mutator) must always have X-Fuzz-Unknown.
	if _, ok := results[1].Headers["X-Fuzz-Unknown"]; !ok {
		t.Error("second result should come from HeaderMutator and have X-Fuzz-Unknown")
	}
}

func TestMutatorSet_MutateAll_DoesNotPanicOnEmptyInput(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
		mutate.NewPathMutator(),
		mutate.NewQueryMutator(),
	)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MutatorSet.MutateAll panicked on empty input: %v", r)
		}
	}()
	set.MutateAll(emptyInput(), rng(1))
}

func TestMutatorSet_MutateAll_IsDeterministic(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
	)
	input := validJSONInput()
	r1 := set.MutateAll(input, rng(77))
	r2 := set.MutateAll(input, rng(77))
	for i := range r1 {
		if string(r1[i].Body) != string(r2[i].Body) {
			t.Errorf("result[%d] body differs between identical seeds", i)
		}
	}
}
