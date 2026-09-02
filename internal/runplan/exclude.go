package runplan

import (
	"strings"

	"github.com/jimbery/bt/pkg/model"
)

// FilterExcludedCases returns cases whose IDs are not listed in excludeCSV.
// excludeCSV is a comma-separated list of case IDs (whitespace trimmed); empty means no filtering.
func FilterExcludedCases(cases []model.Case, excludeCSV string) []model.Case {
	excludeCSV = strings.TrimSpace(excludeCSV)
	if excludeCSV == "" {
		return cases
	}
	ex := make(map[string]struct{})
	for _, id := range strings.Split(excludeCSV, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ex[id] = struct{}{}
		}
	}
	if len(ex) == 0 {
		return cases
	}
	out := cases[:0]
	for _, c := range cases {
		if _, skip := ex[c.ID]; skip {
			continue
		}
		out = append(out, c)
	}
	return out
}
