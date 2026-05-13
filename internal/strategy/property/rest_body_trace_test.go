package property

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/jayimbery/bt/pkg/model"
)

func TestRestJSONObjectWithTrace_ConstrainsCurrencyToProfile(t *testing.T) {
	body := &model.SchemaRef{
		Type: "object",
		Properties: map[string]*model.SchemaRef{
			"currency": {Type: "string"},
		},
		Required: []string{"currency"},
	}
	opProf := &model.OperationProfile{
		Arguments: map[string]*model.ArgumentProfile{
			"currency": {
				Type:         "string",
				Distribution: map[string]float64{"GBP": 0.9, "EUR": 0.1},
			},
		},
	}
	rapid.Check(t, func(rt *rapid.T) {
		v := restJSONObjectWithTrace(rt, body, opProf)
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("want map, got %T", v)
		}
		cur, ok := m["currency"].(string)
		if !ok {
			t.Fatalf("currency: want string, got %T", m["currency"])
		}
		if cur != "GBP" && cur != "EUR" {
			t.Fatalf("currency %q not in trace support", cur)
		}
	})
}

func TestPropertyStrategy_buildCaseInputUsesTrace(t *testing.T) {
	op := model.Operation{
		ID:     "CreateOrder",
		Method: "POST",
		RequestBody: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"currency": {Type: "string"},
			},
			Required: []string{"currency"},
		},
	}
	s := &propertyStrategy{
		opts: Options{
			TraceProfile: &model.TraceProfile{
				SchemaVersion: "1",
				Operations: map[string]*model.OperationProfile{
					"CreateOrder": {
						Arguments: map[string]*model.ArgumentProfile{
							"currency": {
								Type:         "string",
								Distribution: map[string]float64{"GBP": 0.5, "USD": 0.5},
							},
						},
					},
				},
			},
		},
	}
	c := model.Case{Input: model.CaseInput{Method: "POST", Path: "/orders"}}
	rapid.Check(t, func(rt *rapid.T) {
		in := s.buildCaseInput(rt, op, c, nil)
		cur, ok := in.Body.(map[string]any)["currency"].(string)
		if !ok {
			t.Fatalf("body.currency: want string")
		}
		if cur != "GBP" && cur != "USD" {
			t.Fatalf("currency %q not in trace support", cur)
		}
	})
}
