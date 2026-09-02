package analyze_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jimbery/bt/internal/trace/analyze"
	"github.com/jimbery/bt/internal/trace/har"
	"github.com/jimbery/bt/pkg/model"
)

func minimalOps() []model.Operation {
	return []model.Operation{
		{
			ID: "CreateOrder", Method: "POST", Path: "/orders",
			RequestBody: &model.SchemaRef{
				Type: "object",
				Properties: map[string]*model.SchemaRef{
					"amount":      {Type: "integer"},
					"currency":    {Type: "string"},
					"description": {Type: "string", Nullable: true},
				},
				Required: []string{"amount", "currency"},
			},
		},
		{
			ID: "GetOrder", Method: "GET", Path: "/orders/{id}",
			Parameters: []model.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &model.SchemaRef{Type: "string"}},
			},
		},
	}
}

func buildEntries(t *testing.T, ops []struct {
	method, path, reqBody, respBody string
	status                          int
	startedAt                       time.Time
}) []har.HAREntry {
	t.Helper()
	entries := make([]har.HAREntry, len(ops))
	for i, op := range ops {
		entries[i] = har.HAREntry{
			Method:          op.method,
			URL:             "https://api.example.com" + op.path,
			RequestBody:     jsonBytes(op.reqBody),
			ResponseBody:    jsonBytes(op.respBody),
			ResponseStatus:  op.status,
			StartedDateTime: op.startedAt,
		}
	}
	return entries
}

func jsonBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return []byte(s)
}

func TestAnalyze_CallCount_MatchedOperations(t *testing.T) {
	now := time.Now()
	entries := buildEntries(t, []struct {
		method, path, reqBody, respBody string
		status                          int
		startedAt                       time.Time
	}{
		{"POST", "/orders", `{"amount":100,"currency":"GBP"}`, `{"id":"o1"}`, 201, now},
		{"POST", "/orders", `{"amount":200,"currency":"USD"}`, `{"id":"o2"}`, 201, now.Add(time.Second)},
		{"GET", "/orders/o1", "", `{"id":"o1","amount":100}`, 200, now.Add(2 * time.Second)},
	})

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	t.Run("CreateOrder has call count 2", func(t *testing.T) {
		op, ok := profile.Operations["CreateOrder"]
		if !ok {
			t.Fatal("expected CreateOrder in profile")
		}
		if op.CallCount != 2 {
			t.Errorf("CallCount: want 2, got %d", op.CallCount)
		}
	})

	t.Run("GetOrder has call count 1", func(t *testing.T) {
		op, ok := profile.Operations["GetOrder"]
		if !ok {
			t.Fatal("expected GetOrder in profile")
		}
		if op.CallCount != 1 {
			t.Errorf("CallCount: want 1, got %d", op.CallCount)
		}
	})

	t.Run("CreateOrder has frequency rank 1 (most frequent)", func(t *testing.T) {
		if profile.Operations["CreateOrder"].FrequencyRank != 1 {
			t.Errorf("FrequencyRank: want 1, got %d", profile.Operations["CreateOrder"].FrequencyRank)
		}
	})
}

func TestAnalyze_UnmatchedEntries_Skipped(t *testing.T) {
	now := time.Now()
	entries := buildEntries(t, []struct {
		method, path, reqBody, respBody string
		status                          int
		startedAt                       time.Time
	}{
		{"DELETE", "/unknown/path", "", "", 404, now},
		{"POST", "/orders", `{"amount":50,"currency":"EUR"}`, `{"id":"o1"}`, 201, now.Add(time.Second)},
	})

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if _, ok := profile.Operations["UnknownOp"]; ok {
		t.Error("unmatched entry should not produce an operation in the profile")
	}
	if profile.Operations["CreateOrder"].CallCount != 1 {
		t.Errorf("matched entry should have call count 1, got %d", profile.Operations["CreateOrder"].CallCount)
	}
}

func TestAnalyze_Distribution_SufficientSamples_Populated(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 0, 30)
	currencies := make([]string, 0, 30)
	for i := 0; i < 21; i++ {
		currencies = append(currencies, "GBP")
	}
	for i := 0; i < 6; i++ {
		currencies = append(currencies, "USD")
	}
	for i := 0; i < 3; i++ {
		currencies = append(currencies, "EUR")
	}
	for i, c := range currencies {
		entries = append(entries, har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"` + c + `"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		})
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg, ok := profile.Operations["CreateOrder"].Arguments["currency"]
	if !ok {
		t.Fatal("expected 'currency' argument in profile")
	}

	t.Run("distribution is populated with sufficient samples", func(t *testing.T) {
		if len(arg.Distribution) == 0 {
			t.Error("expected non-empty distribution for 30 samples")
		}
	})

	t.Run("GBP has highest probability (~0.70)", func(t *testing.T) {
		if arg.Distribution["GBP"] < 0.60 || arg.Distribution["GBP"] > 0.80 {
			t.Errorf("GBP distribution: want ~0.70, got %f", arg.Distribution["GBP"])
		}
	})

	t.Run("distribution sums to 1.0", func(t *testing.T) {
		total := 0.0
		for _, p := range arg.Distribution {
			total += p
		}
		if total < 0.999 || total > 1.001 {
			t.Errorf("distribution does not sum to 1.0: got %f", total)
		}
	})
}

func TestAnalyze_Distribution_InsufficientSamples_NotPopulated(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 5)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg := profile.Operations["CreateOrder"].Arguments["currency"]
	if len(arg.Distribution) != 0 {
		t.Errorf("expected empty distribution for fewer than 20 samples, got: %v", arg.Distribution)
	}
	if len(arg.Samples) != 5 {
		t.Errorf("expected 5 samples, got %d", len(arg.Samples))
	}
}

func TestAnalyze_Range_NumericArgument_Populated(t *testing.T) {
	now := time.Now()
	amounts := []int{10, 50, 100, 200, 500, 25, 75, 150, 300, 400,
		12, 60, 110, 210, 510, 35, 85, 160, 310, 410,
		15, 55, 105}
	entries := make([]har.HAREntry, len(amounts))
	for i, a := range amounts {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":` + itoa(a) + `,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg := profile.Operations["CreateOrder"].Arguments["amount"]

	t.Run("range is non-nil for numeric argument", func(t *testing.T) {
		if arg.Range == nil {
			t.Fatal("expected non-nil Range for integer argument 'amount'")
		}
	})
	t.Run("range min is correct", func(t *testing.T) {
		if arg.Range.Min != 10 {
			t.Errorf("Range.Min: want 10, got %f", arg.Range.Min)
		}
	})
	t.Run("range max is correct", func(t *testing.T) {
		if arg.Range.Max != 510 {
			t.Errorf("Range.Max: want 510, got %f", arg.Range.Max)
		}
	})
}

func TestAnalyze_Range_StringArgument_Nil(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalOps(), "x.har")
	arg := profile.Operations["CreateOrder"].Arguments["currency"]
	if arg.Range != nil {
		t.Errorf("expected nil Range for string argument 'currency', got: %+v", arg.Range)
	}
}

func TestAnalyze_NullRate_CalculatedFromObservations(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 20)
	for i := range entries {
		if i%2 == 0 {
			entries[i] = har.HAREntry{
				Method: "POST", URL: "https://api.example.com/orders",
				RequestBody:     []byte(`{"amount":100,"currency":"GBP","description":"test"}`),
				ResponseStatus:  201,
				StartedDateTime: now.Add(time.Duration(i) * time.Second),
			}
		} else {
			entries[i] = har.HAREntry{
				Method: "POST", URL: "https://api.example.com/orders",
				RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
				ResponseStatus:  201,
				StartedDateTime: now.Add(time.Duration(i) * time.Second),
			}
		}
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg, ok := profile.Operations["CreateOrder"].Arguments["description"]
	if !ok {
		t.Fatal("expected description argument")
	}

	t.Run("null rate is approximately 0.50", func(t *testing.T) {
		if arg.NullRate < 0.40 || arg.NullRate > 0.60 {
			t.Errorf("NullRate: want ~0.50, got %f", arg.NullRate)
		}
	})
}

func TestAnalyze_AlwaysPresent_WhenFieldAppearsInAllEntries(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method: "POST", URL: "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalOps(), "x.har")
	currencyArg := profile.Operations["CreateOrder"].Arguments["currency"]

	if !currencyArg.AlwaysPresent {
		t.Error("expected AlwaysPresent=true for 'currency' which appears in every request")
	}
}

func TestAnalyze_AlwaysPresent_FalseWhenFieldSometimesAbsent(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		body := `{"amount":100,"currency":"GBP"}`
		if i%5 == 0 {
			body = `{"amount":100}`
		}
		entries[i] = har.HAREntry{
			Method: "POST", URL: "https://api.example.com/orders",
			RequestBody:     []byte(body),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalOps(), "x.har")
	currencyArg := profile.Operations["CreateOrder"].Arguments["currency"]

	if currencyArg.AlwaysPresent {
		t.Error("expected AlwaysPresent=false for 'currency' which is sometimes absent")
	}
}

func TestAnalyze_SchemaVersion_IsOne(t *testing.T) {
	profile, err := analyze.Analyze(nil, minimalOps(), "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if profile.SchemaVersion != "1" {
		t.Errorf("SchemaVersion: want '1', got %q", profile.SchemaVersion)
	}
}

func TestAnalyze_GeneratedAt_IsRFC3339(t *testing.T) {
	profile, err := analyze.Analyze(nil, minimalOps(), "")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, profile.GeneratedAt); err != nil {
		t.Errorf("GeneratedAt %q is not RFC3339: %v", profile.GeneratedAt, err)
	}
}

func TestAnalyze_Sequences_StartProbability_SumsToOne(t *testing.T) {
	now := time.Now()
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
		{Method: "GET", URL: "https://api.example.com/orders/o1", ResponseStatus: 200, StartedDateTime: now.Add(5 * time.Second)},
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":50,"currency":"USD"}`), ResponseStatus: 201, StartedDateTime: now.Add(2000 * time.Second)},
		{Method: "GET", URL: "https://api.example.com/orders/o2", ResponseStatus: 200, StartedDateTime: now.Add(2005 * time.Second)},
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if profile.Sequences == nil {
		t.Fatal("expected non-nil Sequences")
	}

	total := 0.0
	for _, p := range profile.Sequences.StartProbability {
		total += p
	}
	if total < 0.999 || total > 1.001 {
		t.Errorf("StartProbability does not sum to 1.0: got %f", total)
	}
}

func TestAnalyze_Sequences_TransitionRows_SumToOne(t *testing.T) {
	now := time.Now()
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
		{Method: "GET", URL: "https://api.example.com/orders/o1", ResponseStatus: 200, StartedDateTime: now.Add(5 * time.Second)},
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":50,"currency":"USD"}`), ResponseStatus: 201, StartedDateTime: now.Add(2000 * time.Second)},
		{Method: "GET", URL: "https://api.example.com/orders/o2", ResponseStatus: 200, StartedDateTime: now.Add(2005 * time.Second)},
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	for opID, row := range profile.Sequences.Transitions {
		total := 0.0
		for _, p := range row {
			total += p
		}
		if total < 0.999 || total > 1.001 {
			t.Errorf("transition row for %q does not sum to 1.0: got %f", opID, total)
		}
	}
}

func TestAnalyze_Sequences_CreateOrder_HasENDSentinel(t *testing.T) {
	now := time.Now()
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
	}

	profile, err := analyze.Analyze(entries, minimalOps(), "x.har")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if profile.Sequences == nil {
		t.Fatal("expected Sequences to be non-nil")
	}
	row, ok := profile.Sequences.Transitions["CreateOrder"]
	if !ok {
		t.Fatal("expected CreateOrder in Transitions")
	}
	if _, hasEnd := row["__END__"]; !hasEnd {
		t.Error("expected __END__ sentinel in CreateOrder transition row")
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
