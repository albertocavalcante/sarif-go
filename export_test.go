package sarif

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	sarif "github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"
)

func TestReportContract(t *testing.T) {
	tool := Tool{Name: "test-linter", Version: "dev", RootURI: "file:///checkout/"}
	input := []Finding{
		{RuleID: "z", Path: `src\space #%.star`, Message: "second", Level: "note", Line: 3, Column: 5, EndLine: 3, EndColumn: 6},
		{RuleID: "a", Path: "a.star", Message: "first", Level: "warning", Line: 1},
	}
	original := slices.Clone(input)
	report, err := Build(tool, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatal("mutated caller input")
	}
	var out bytes.Buffer
	if err := Write(&out, report); err != nil {
		t.Fatal(err)
	}
	// Validate the serialized output without repairing null or missing arrays.
	parsed, err := sarif.FromBytes(out.Bytes(), sarif.WithStrictValidation())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatal(err)
	}
	run := parsed.Runs[0]
	if *run.ColumnKind != "utf16CodeUnits" {
		t.Fatal("column encoding absent")
	}
	if *run.Tool.Driver.Rules[0].ID != "a" || *run.Tool.Driver.Rules[1].ID != "z" {
		t.Fatal("rules not sorted")
	}
	for _, r := range run.Results {
		if *r.RuleID != *run.Tool.Driver.Rules[r.RuleIndex].ID {
			t.Fatal("invalid rule index")
		}
	}
	location := run.Results[1].Locations[0].PhysicalLocation.ArtifactLocation
	if *location.URI != "src/space%20%23%25.star" {
		t.Fatalf("URI: %s", *location.URI)
	}
	if *location.URIBaseID != "%SRCROOT%" {
		t.Fatal("missing URI base")
	}
	slices.Reverse(input)
	again, err := Build(tool, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reversed bytes.Buffer
	if err := Write(&reversed, again); err != nil {
		t.Fatal(err)
	}
	if out.String() != reversed.String() {
		t.Fatal("output depends on input order")
	}
}

func TestEmptyAndIncompleteRuns(t *testing.T) {
	for _, failures := range [][]string{nil, {"cannot read source"}} {
		r, err := Build(Tool{Name: "lint", RootURI: "file:///checkout/"}, nil, failures)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r)
		if !strings.Contains(string(b), `"results":[]`) {
			t.Fatalf("empty scan must serialize an array: %s", b)
		}
		if *r.Runs[0].Invocations[0].ExecutionSuccessful != (len(failures) == 0) {
			t.Fatal("failure reported as success")
		}
	}
}

func TestInvalidFindings(t *testing.T) {
	base := Finding{RuleID: "rule", Path: "src/main.go", Message: "message", Level: "warning", Line: 1}
	cases := []struct {
		name   string
		change func(*Finding)
	}{
		{"empty rule", func(f *Finding) { f.RuleID = "" }},
		{"empty message", func(f *Finding) { f.Message = "" }},
		{"severity", func(f *Finding) { f.Level = "high" }},
		{"absolute", func(f *Finding) { f.Path = "/tmp/main.go" }},
		{"windows absolute", func(f *Finding) { f.Path = `C:\src\main.go` }},
		{"escape", func(f *Finding) { f.Path = "../main.go" }},
		{"empty path", func(f *Finding) { f.Path = "" }},
		{"nul", func(f *Finding) { f.Path = "src/\x00.go" }},
		{"line zero", func(f *Finding) { f.Line = 0 }},
		{"column negative", func(f *Finding) { f.Column = -1 }},
		{"backwards lines", func(f *Finding) { f.Line = 2; f.EndLine = 1 }},
		{"backwards columns", func(f *Finding) { f.Column = 4; f.EndLine = 1; f.EndColumn = 3 }},
		{"end without start", func(f *Finding) { f.EndLine = 1; f.EndColumn = 3 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.change(&f)
			if _, err := Build(Tool{Name: "lint", RootURI: "file:///checkout/"}, []Finding{f}, nil); err == nil {
				t.Fatal("accepted invalid finding")
			}
		})
	}
	for _, root := range []string{"", "checkout/", "file:///checkout", "https://example.com/", "file:///checkout/?x=1"} {
		if _, err := Build(Tool{Name: "lint", RootURI: root}, nil, nil); err == nil {
			t.Fatalf("accepted invalid root %q", root)
		}
	}
}

func TestPosition(t *testing.T) {
	src := []byte("é😀\tX\r\nY\rZ\n")
	cases := []struct{ offset, line, col int }{{0, 1, 1}, {2, 1, 2}, {6, 1, 4}, {7, 1, 5}, {8, 1, 6}, {10, 2, 1}, {11, 2, 2}, {12, 3, 1}, {14, 4, 1}}
	for _, tc := range cases {
		line, col, err := Position(src, tc.offset)
		if err != nil || line != tc.line || col != tc.col {
			t.Errorf("offset %d: (%d,%d,%v), want (%d,%d)", tc.offset, line, col, err, tc.line, tc.col)
		}
	}
	for _, offset := range []int{-1, 1, 3, 4, 5, 9, 15} {
		if _, _, err := Position(src, offset); err == nil {
			t.Errorf("accepted offset %d", offset)
		}
	}
	if _, _, err := Position([]byte{0xff}, 0); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }
func TestWriteFailure(t *testing.T) {
	r, _ := Build(Tool{Name: "lint", RootURI: "file:///checkout/"}, nil, nil)
	if err := Write(brokenWriter{}, r); err == nil {
		t.Fatal("discarded writer failure")
	}
}
