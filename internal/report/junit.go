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
	suite := JUnitTestSuite{
		Name:     "bt",
		Tests:    s.Total,
		Failures: s.Failed,
	}

	for _, res := range results {
		tc := JUnitTestCase{
			Name:      res.CaseID,
			Classname: "bt",
		}
		if res.Skipped {
			suite.Skipped++
			tc.Skipped = &JUnitSkipped{Message: res.SkipReason}
		} else if !res.Passed && len(res.Failures) > 0 {
			msgs := ""
			for _, f := range res.Failures {
				label := f.Invariant
				if f.Classification != "" {
					label = f.Classification
				}
				msgs += fmt.Sprintf("%s: %s\n", label, f.Message)
			}
			tc.Failure = &JUnitFailure{
				Message: res.Failures[0].Message,
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
