package table

import (
	"testing"

	"github.com/jayimbery/bt/pkg/model"
)

func TestMergeCaseInputHeaders_caseOverridesDefault(t *testing.T) {
	t.Parallel()
	in := model.CaseInput{
		Headers: map[string]string{"Authorization": "Bearer from-case"},
	}
	out := mergeCaseInputHeaders(in, map[string]string{
		"Authorization": "Bearer from-default",
		"X-Extra":       "keep",
	})
	if out.Headers["Authorization"] != "Bearer from-case" {
		t.Errorf("Authorization: got %q", out.Headers["Authorization"])
	}
	if out.Headers["X-Extra"] != "keep" {
		t.Errorf("X-Extra: got %q", out.Headers["X-Extra"])
	}
}
