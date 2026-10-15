// Package goanalysis adapts native go/analysis diagnostics without executing
// analyzers. Filesystem access is supplied explicitly by the caller.
package goanalysis

import (
	"fmt"
	"go/token"

	sarif "github.com/albertocavalcante/sarif-go"
	"golang.org/x/tools/go/analysis"
)

type Adapter struct {
	Root     string
	Files    *token.FileSet
	ReadFile func(string) ([]byte, error)
}

func (a Adapter) Finding(ruleID, level string, d analysis.Diagnostic) (sarif.Finding, error) {
	if a.Files == nil || a.ReadFile == nil {
		return sarif.Finding{}, fmt.Errorf("file set and source reader are required")
	}
	location := func(start, end token.Pos) (string, sarif.Region, error) {
		file := a.Files.File(start)
		if file == nil {
			return "", sarif.Region{}, fmt.Errorf("diagnostic position has no source file")
		}
		if end == token.NoPos {
			end = start
		}
		if end < start || a.Files.File(end) != file {
			return "", sarif.Region{}, fmt.Errorf("diagnostic span crosses files or runs backwards")
		}
		content, err := a.ReadFile(file.Name())
		if err != nil {
			return "", sarif.Region{}, fmt.Errorf("read diagnostic source: %w", err)
		}
		source, err := sarif.NewSource(content)
		if err != nil {
			return "", sarif.Region{}, err
		}
		region, err := source.Region(file.Offset(start), file.Offset(end))
		if err != nil {
			return "", sarif.Region{}, err
		}
		path, err := sarif.RelativePath(a.Root, file.Name())
		return path, region, err
	}
	path, region, err := location(d.Pos, d.End)
	if err != nil {
		return sarif.Finding{}, err
	}
	f := sarif.Finding{RuleID: ruleID, Message: d.Message, Level: level, Path: path, Line: region.Line, Column: region.Column, EndLine: region.EndLine, EndColumn: region.EndColumn}
	// A point finding omits the unknown extent, rather than inventing a character.
	if d.End == token.NoPos || d.End == d.Pos {
		f.EndLine = 0
		f.EndColumn = 0
	}
	if d.Category != "" {
		f.Properties = map[string]any{"category": d.Category}
	}
	for _, r := range d.Related {
		path, region, err := location(r.Pos, r.End)
		if err != nil {
			return sarif.Finding{}, err
		}
		f.Related = append(f.Related, sarif.RelatedLocation{Path: path, Message: r.Message, Region: region})
	}
	for _, suggestion := range d.SuggestedFixes {
		fix := sarif.Fix{Description: suggestion.Message}
		for _, edit := range suggestion.TextEdits {
			path, region, err := location(edit.Pos, edit.End)
			if err != nil {
				return sarif.Finding{}, err
			}
			fix.Edits = append(fix.Edits, sarif.Edit{Path: path, Region: region, Text: string(edit.NewText)})
		}
		f.Fixes = append(f.Fixes, fix)
	}
	return f, nil
}
