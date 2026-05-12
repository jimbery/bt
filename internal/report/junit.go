package report

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/jayimbery/bt/pkg/model"
)

// JUnitTestSuites is the root XML element.
type JUnitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a single test suite.
type JUnitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Skipped  int             `xml:"skipped,attr,omitempty"`
	Cases    []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a single test case.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

// JUnitSkipped marks a skipped test case.
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnitFailure represents a test failure.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitReporter struct{ w io.Writer }

// NewJUnit returns a Reporter that writes JUnit-compatible XML.
func NewJUnit(w io.Writer) Reporter { return &junitReporter{w: w} }

func (r *junitReporter) Write(results []model.Result) error {
	s := summarise(results)
	failures := 0
	for _, res := range results {
		if !res.Skipped && !res.Passed && !res.Quarantined && (len(res.Failures) > 0 || len(res.SchemaViolations) > 0) {
			failures++
		}
	}
	suite := JUnitTestSuite{
		Name:     "bt",
		Tests:    s.Total,
		Failures: failures,
	}

	for _, res := range results {
		tc := JUnitTestCase{
			Name:      res.CaseID,
			Classname: "bt",
		}
		if res.Skipped {
			suite.Skipped++
			tc.Skipped = &JUnitSkipped{Message: res.SkipReason}
		} else if !res.Passed && !res.Quarantined && (len(res.Failures) > 0 || len(res.SchemaViolations) > 0) {
			msgs := ""
			for _, f := range res.Failures {
				label := f.Invariant
				if f.Classification != "" {
					label = f.Classification
				}
				msgs += fmt.Sprintf("%s: %s\n", label, f.Message)
			}
			for _, sv := range res.SchemaViolations {
				msgs += fmt.Sprintf("schema: %s: %s [%s]\n", sv.Field, sv.Message, sv.Severity)
			}
			if len(res.Response.Body) > 0 {
				msgs += "\n--- response body ---\n" + FailureBodyForDisplay(res.Response.Body) + "\n"
			}
			msg := ""
			if len(res.Failures) > 0 {
				msg = res.Failures[0].Message
			} else if len(res.SchemaViolations) > 0 {
				msg = res.SchemaViolations[0].Message
			}
			tc.Failure = &JUnitFailure{
				Message: msg,
				Text:    msgs,
			}
		}
		suite.Cases = append(suite.Cases, tc)
	}

	suites := JUnitTestSuites{Suites: []JUnitTestSuite{suite}}
	out, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal JUnit XML: %w", err)
	}
	_, err = r.w.Write(out)
	return err
}
