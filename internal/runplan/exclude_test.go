package runplan

import (
	"testing"

	"github.com/jayimbery/bt/pkg/model"
)

func TestFilterExcludedCases(t *testing.T) {
	cases := []model.Case{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	got := FilterExcludedCases(cases, " b , ")
	want := []string{"a", "c"}
	if len(got) != len(want) {
		t.Fatalf("len got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("[%d]: got %q want %q", i, got[i].ID, want[i])
		}
	}
}

func TestFilterExcludedCases_EmptyAndNil(t *testing.T) {
	cases := []model.Case{{ID: "x"}}
	if len(FilterExcludedCases(cases, "")) != 1 {
		t.Fatal("empty exclude must keep all")
	}
	if len(FilterExcludedCases(cases, "   ")) != 1 {
		t.Fatal("whitespace-only exclude must keep all")
	}
}
