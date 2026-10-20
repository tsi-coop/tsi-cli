package tsitest

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// WriteHuman prints results as a checklist.
func WriteHuman(w io.Writer, results []Result) (passed, failed int) {
	skipped := 0
	for _, r := range results {
		for _, c := range r.Checks {
			mark := "✔"
			if c.Skipped {
				mark = "–"
			} else if !c.Passed {
				mark = "✘"
			}
			fmt.Fprintf(w, "%s %-22s %-24s %s\n", mark, r.Module, c.ID, c.Description)
			if c.Error != "" {
				fmt.Fprintf(w, "    %s\n", strings.ReplaceAll(c.Error, "\n", "\n    "))
			}
		}
		passed += r.Passed()
		failed += r.Failed()
		skipped += r.Skipped()
	}
	if skipped > 0 {
		fmt.Fprintf(w, "%d passed, %d failed, %d skipped (not applicable to that node)\n", passed, failed, skipped)
	} else {
		fmt.Fprintf(w, "%d passed, %d failed\n", passed, failed)
	}
	return
}

// WriteJSON prints results as JSON.
func WriteJSON(w io.Writer, results []Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Time     float64     `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name    string        `xml:"name,attr"`
	Class   string        `xml:"classname,attr"`
	Time    float64       `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
	Skipped *struct{}     `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit prints results as JUnit XML.
func WriteJUnit(w io.Writer, results []Result) error {
	var js junitSuites
	for _, r := range results {
		s := junitSuite{Name: r.Module, Tests: len(r.Checks), Failures: r.Failed(), Time: r.Duration.Seconds()}
		for _, c := range r.Checks {
			jc := junitCase{Name: c.ID, Class: r.Module, Time: c.Duration.Seconds()}
			if c.Skipped {
				jc.Skipped = &struct{}{}
			} else if !c.Passed {
				jc.Failure = &junitFailure{Message: c.Error}
			}
			s.Cases = append(s.Cases, jc)
		}
		js.Suites = append(js.Suites, s)
	}
	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(js)
}
