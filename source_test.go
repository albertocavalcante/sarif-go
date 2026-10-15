package sarif

import (
	"bytes"
	"testing"
)

func TestSourceCoordinates(t *testing.T) {
	s, err := NewSource([]byte("é😀\tX\r\nY\rZ\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ offset, line, col int }{{0, 1, 1}, {2, 1, 2}, {6, 1, 4}, {7, 1, 5}, {8, 1, 6}, {10, 2, 1}, {11, 2, 2}, {12, 3, 1}, {14, 4, 1}} {
		l, c, err := s.At(tc.offset)
		if err != nil || l != tc.line || c != tc.col {
			t.Errorf("%+v: %d:%d %v", tc, l, c, err)
		}
	}
	r, err := s.RuneRegion(1, 3, 1, 4)
	if err != nil || r.Column != 4 || r.EndColumn != 5 {
		t.Fatalf("rune region %+v: %v", r, err)
	}
	for _, pos := range []int{-1, 1, 3, 4, 5, 9, 15} {
		if _, _, err := s.At(pos); err == nil {
			t.Errorf("accepted invalid offset %d", pos)
		}
	}
	if _, err := s.Region(7, 6); err == nil {
		t.Fatal("accepted backwards span")
	}
	if _, err := s.RuneRegion(1, 20, 0, 0); err == nil {
		t.Fatal("accepted out of bounds column")
	}
}

func TestMetadataAndFixes(t *testing.T) {
	type testCase struct{ invalid bool }
	for _, tc := range []testCase{{false}, {true}} {
		f := Finding{RuleID: "rule", Path: "a.go", Message: "message", Level: "warning", Line: 1, Related: []RelatedLocation{{Path: "b.go", Message: "defined here", Region: Region{Line: 2, Column: 1}}}, Properties: map[string]any{"required_version": "3.20"}, Fixes: []Fix{{Description: "add import and replace", Edits: []Edit{{Path: "a.go", Region: Region{1, 1, 1, 1}, Text: "import x\n"}, {Path: "a.go", Region: Region{2, 1, 2, 4}, Text: "new"}}}}}
		if tc.invalid {
			f.Fixes[0].Edits[0].Region = Region{1, 1, 1, 4}
			f.Fixes[0].Edits[1].Region = Region{1, 2, 1, 3}
		}
		r, err := Build(Tool{Name: "lint", RootURI: "file:///checkout/", Rules: []Rule{{ID: "rule", Description: "A stable description", Help: "How to fix", HelpURI: "https://example.com/rule"}}}, []Finding{f}, nil)
		if tc.invalid {
			if err == nil {
				t.Fatal("accepted overlapping edits")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := Write(&out, r); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"required_version": "3.20"`)) {
			t.Fatal("lost properties")
		}
	}
}

func TestInvalidReplacementUTF8(t *testing.T) {
	f := Finding{RuleID: "r", Path: "a.go", Message: "m", Level: "warning", Line: 1, Fixes: []Fix{{Edits: []Edit{{Path: "a.go", Region: Region{1, 1, 1, 1}, Text: string([]byte{0xff})}}}}}
	if _, err := Build(Tool{Name: "test", RootURI: "file:///checkout/"}, []Finding{f}, nil); err == nil {
		t.Fatal("invalid UTF-8 edit would be corrupted during JSON serialization")
	}
}
