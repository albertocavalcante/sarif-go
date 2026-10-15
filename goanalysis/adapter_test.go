package goanalysis

import (
	"go/token"
	"testing"

	sarif "github.com/albertocavalcante/sarif-go"
	"golang.org/x/tools/go/analysis"
)

func TestNativeDiagnostic(t *testing.T) {
	content := []byte("package a\n// 😀\nvar x = 1\n")
	files := token.NewFileSet()
	file := files.AddFile("/checkout/a.go", -1, len(content))
	file.SetLinesForContent(content)
	a := Adapter{Root: "/checkout", Files: files, ReadFile: func(string) ([]byte, error) { return content, nil }}
	d := analysis.Diagnostic{Pos: file.Pos(23), Message: "replace", Category: "style", Related: []analysis.RelatedInformation{{Pos: file.Pos(0), Message: "context"}}, SuggestedFixes: []analysis.SuggestedFix{{Message: "insert", TextEdits: []analysis.TextEdit{{Pos: file.Pos(0), End: file.Pos(0), NewText: []byte("// inserted\n")}}}}}
	f, err := a.Finding("rule", "warning", d)
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "a.go" || len(f.Related) != 1 || len(f.Fixes) != 1 || f.Fixes[0].Edits[0].Text != "// inserted\n" {
		t.Fatalf("lost diagnostic data: %+v", f)
	}
	r, err := sarif.Build(sarif.Tool{Name: "test", RootURI: "file:///checkout/"}, []sarif.Finding{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	d.Pos = token.NoPos
	if _, err := a.Finding("rule", "warning", d); err == nil {
		t.Fatal("accepted missing position")
	}
}

func TestOrderedInsertions(t *testing.T) {
	content := []byte("package a\n")
	files := token.NewFileSet()
	file := files.AddFile("/checkout/a.go", -1, len(content))
	file.SetLinesForContent(content)
	a := Adapter{Root: "/checkout", Files: files, ReadFile: func(string) ([]byte, error) { return content, nil }}
	d := analysis.Diagnostic{Pos: file.Pos(0), Message: "add comments", SuggestedFixes: []analysis.SuggestedFix{{Message: "insert", TextEdits: []analysis.TextEdit{{Pos: file.Pos(0), End: file.Pos(0), NewText: []byte("// first\n")}, {Pos: file.Pos(0), End: file.Pos(0), NewText: []byte("// second\n")}}}}}
	f, err := a.Finding("rule", "warning", d)
	if err != nil {
		t.Fatal(err)
	}
	r, err := sarif.Build(sarif.Tool{Name: "test", RootURI: "file:///checkout/"}, []sarif.Finding{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	edits := r.Runs[0].Results[0].Fixes[0].ArtifactChanges[0].Replacements
	if len(edits) != 1 || *edits[0].InsertedContent.Text != "// first\n// second\n" {
		t.Fatalf("lost native insertion ordering: %+v", edits)
	}
}
