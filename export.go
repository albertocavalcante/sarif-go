// Package sarif is a producer for SARIF 2.1.0 reports.
// Tool-specific analyzers and filesystem operations stay outside this package.
package sarif

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	schema "github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"
)

type Tool struct {
	Name, Version  string
	InformationURI string
	Rules          []Rule
	// RootURI is an absolute file URI ending in '/', identifying the checkout.
	RootURI string
}

// Finding uses repository-relative paths and one-based positions. Columns are
// UTF-16 code units; zero means unknown. End positions are exclusive.
// RuleID is the analyzer's stable identifier, never its diagnostic message.
type Finding struct {
	RuleID, Path, Message, Level     string
	Line, Column, EndLine, EndColumn int
	Related                          []RelatedLocation
	Fixes                            []Fix
	Properties                       map[string]any
}

// Rule carries stable metadata shared by all results of an analyzer.
type Rule struct{ ID, Name, Description, Help, HelpURI string }

// Region uses one-based lines and UTF-16 columns, with exclusive ends.
// Equal start and end positions represent an insertion in a suggested edit.
type Region struct{ Line, Column, EndLine, EndColumn int }
type RelatedLocation struct {
	Path, Message string
	Region        Region
}
type Fix struct {
	Description string
	Edits       []Edit
}
type Edit struct {
	Path, Text string
	Region     Region
}

// Build constructs one run without IO or mutating the caller's findings.
// Failures are tool execution failures, distinct from source findings.
func Build(tool Tool, findings []Finding, failures []string) (*schema.Report, error) {
	if strings.TrimSpace(tool.Name) == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	root, err := url.Parse(tool.RootURI)
	if err != nil || root.Scheme != "file" || !strings.HasPrefix(root.Path, "/") || !strings.HasSuffix(root.Path, "/") || root.RawQuery != "" || root.Fragment != "" || root.User != nil {
		return nil, fmt.Errorf("root must be an absolute file URI ending in /")
	}
	items := slices.Clone(findings)
	ruleIDs := make(map[string]bool)
	rules := make(map[string]Rule)
	for _, rule := range tool.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return nil, fmt.Errorf("rule ID is required")
		}
		if _, ok := rules[rule.ID]; ok {
			return nil, fmt.Errorf("duplicate rule %q", rule.ID)
		}
		rules[rule.ID] = rule
		ruleIDs[rule.ID] = true
	}
	for i := range items {
		f := &items[i]
		if _, err := json.Marshal(f); err != nil {
			return nil, fmt.Errorf("finding %d: invalid properties: %w", i, err)
		}
		if strings.TrimSpace(f.RuleID) == "" || strings.TrimSpace(f.Message) == "" {
			return nil, fmt.Errorf("finding %d: rule and message are required", i)
		}
		if f.Level != "error" && f.Level != "warning" && f.Level != "note" && f.Level != "none" {
			return nil, fmt.Errorf("finding %d: unsupported severity %q", i, f.Level)
		}
		f.Path = strings.ReplaceAll(f.Path, "\\", "/")
		f.Path = path.Clean(f.Path)
		if f.Path == "." || f.Path == ".." || strings.HasPrefix(f.Path, "../") || strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, ":") || strings.ContainsRune(f.Path, 0) || !utf8.ValidString(f.Path) {
			return nil, fmt.Errorf("finding %d: path must be repository-relative", i)
		}
		if f.Line < 1 || f.Column < 0 || f.EndLine < 0 || f.EndColumn < 0 || (f.EndColumn > 0 && (f.EndLine == 0 || f.Column == 0)) || (f.EndLine > 0 && f.EndLine < f.Line) || (f.EndLine == f.Line && f.EndColumn > 0 && f.EndColumn <= f.Column) {
			return nil, fmt.Errorf("finding %d: invalid source region", i)
		}
		ruleIDs[f.RuleID] = true
	}
	slices.SortFunc(items, func(a, b Finding) int {
		order := cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), cmp.Compare(a.RuleID, b.RuleID), cmp.Compare(a.Message, b.Message), cmp.Compare(a.Level, b.Level), cmp.Compare(a.EndLine, b.EndLine), cmp.Compare(a.EndColumn, b.EndColumn))
		if order != 0 {
			return order
		}
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return bytes.Compare(left, right)
	})
	run := schema.NewRun()
	run.Tool = schema.NewTool().WithDriver(schema.NewToolComponent().WithName(tool.Name))
	if tool.Version != "" {
		run.Tool.Driver.WithVersion(tool.Version)
	}
	if tool.InformationURI != "" {
		run.Tool.Driver.WithInformationURI(tool.InformationURI)
	}
	run.WithColumnKind("utf16CodeUnits")
	run.WithNewlineSequences([]string{"\r\n", "\r", "\n"})
	run.OriginalUriBaseIds = map[string]schema.ArtifactLocation{
		"%SRCROOT%": *schema.NewArtifactLocation().WithURI(tool.RootURI),
	}
	invocation := schema.NewInvocation().WithExecutionSuccessful(len(failures) == 0)
	for _, failure := range slices.Sorted(slices.Values(failures)) {
		invocation.AddToolExecutionNotification(schema.NewNotification().WithLevel("error").WithMessage(schema.NewTextMessage(failure)))
	}
	run.AddInvocation(invocation)
	ids := make([]string, 0, len(ruleIDs))
	for id := range ruleIDs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r := run.AddRule(id)
		metadata := rules[id]
		if metadata.Name != "" {
			r.WithName(metadata.Name)
		}
		if metadata.Description != "" {
			r.WithShortDescription(schema.NewMultiformatMessageString().WithText(metadata.Description))
		}
		if metadata.Help != "" {
			r.WithHelp(schema.NewMultiformatMessageString().WithText(metadata.Help))
		}
		if metadata.HelpURI != "" {
			r.WithHelpURI(metadata.HelpURI)
		}
	}
	for _, f := range items {
		region := schema.NewRegion().WithStartLine(f.Line)
		if f.Column > 0 {
			region.WithStartColumn(f.Column)
		}
		if f.EndLine > 0 {
			region.WithEndLine(f.EndLine)
		}
		if f.EndColumn > 0 {
			region.WithEndColumn(f.EndColumn)
		}
		uri := (&url.URL{Path: f.Path}).EscapedPath()
		location := schema.NewLocationWithPhysicalLocation(schema.NewPhysicalLocation().
			WithArtifactLocation(schema.NewArtifactLocation().WithURI(uri).WithURIBaseID("%SRCROOT%")).WithRegion(region))
		result := run.CreateResultForRule(f.RuleID).WithLevel(f.Level).WithMessage(schema.NewTextMessage(f.Message)).AddLocation(location)
		for i, related := range f.Related {
			loc, err := makeLocation(related.Path, related.Region)
			if err != nil {
				return nil, fmt.Errorf("related location: %w", err)
			}
			loc.WithID(i + 1).WithMessage(schema.NewTextMessage(related.Message))
			result.AddRelatedLocation(loc)
		}
		for _, fix := range f.Fixes {
			if len(fix.Edits) == 0 {
				return nil, fmt.Errorf("fix must contain edits")
			}
			changes := map[string]*schema.ArtifactChange{}
			for _, edit := range fix.Edits {
				if !utf8.ValidString(edit.Text) {
					return nil, fmt.Errorf("replacement text must be valid UTF-8")
				}
				loc, err := makeLocation(edit.Path, edit.Region)
				if err != nil {
					return nil, fmt.Errorf("fix location: %w", err)
				}
				if edit.Region.Column < 1 || edit.Region.EndLine < 1 || edit.Region.EndColumn < 1 {
					return nil, fmt.Errorf("edit requires complete source region")
				}
				key := *loc.PhysicalLocation.ArtifactLocation.URI
				if changes[key] == nil {
					changes[key] = schema.NewArtifactChange().WithArtifactLocation(loc.PhysicalLocation.ArtifactLocation)
				}
				changes[key].AddReplacement(schema.NewReplacement().WithDeletedRegion(loc.PhysicalLocation.Region).WithInsertedContent(schema.NewArtifactContent().WithText(edit.Text)))
			}
			value := schema.NewFix().WithDescription(schema.NewTextMessage(fix.Description))
			keys := make([]string, 0, len(changes))
			for key := range changes {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				replacements := changes[key].Replacements
				slices.SortStableFunc(replacements, func(a, b *schema.Replacement) int {
					return cmp.Or(cmp.Compare(*a.DeletedRegion.StartLine, *b.DeletedRegion.StartLine), cmp.Compare(*a.DeletedRegion.StartColumn, *b.DeletedRegion.StartColumn), cmp.Compare(*a.DeletedRegion.EndLine, *b.DeletedRegion.EndLine), cmp.Compare(*a.DeletedRegion.EndColumn, *b.DeletedRegion.EndColumn))
				})
				merged := make([]*schema.Replacement, 0, len(replacements))
				for _, current := range replacements {
					if len(merged) > 0 {
						previous := merged[len(merged)-1]
						a, b := previous.DeletedRegion, current.DeletedRegion
						if *a.StartLine == *b.StartLine && *a.StartColumn == *b.StartColumn && *a.EndLine == *a.StartLine && *a.EndColumn == *a.StartColumn {
							text := *previous.InsertedContent.Text + *current.InsertedContent.Text
							current.InsertedContent.WithText(text)
							merged[len(merged)-1] = current
							continue
						}
						if *a.EndLine > *b.StartLine || (*a.EndLine == *b.StartLine && *a.EndColumn > *b.StartColumn) {
							return nil, fmt.Errorf("fix contains overlapping edits")
						}
					}
					merged = append(merged, current)
				}
				changes[key].Replacements = merged
				value.AddArtifactChange(changes[key])
			}
			result.Fixes = append(result.Fixes, value)
		}
		if len(f.Properties) > 0 {
			bag := schema.NewPropertyBag()
			for key, value := range f.Properties {
				bag.Add(key, value)
			}
			result.WithProperties(bag)
		}
	}
	return schema.NewReport().AddRun(run), nil
}

func makeLocation(file string, r Region) (*schema.Location, error) {
	file = strings.ReplaceAll(file, "\\", "/")
	file = path.Clean(file)
	if file == "." || file == ".." || strings.HasPrefix(file, "../") || strings.HasPrefix(file, "/") || strings.Contains(file, ":") || strings.ContainsRune(file, 0) || !utf8.ValidString(file) {
		return nil, fmt.Errorf("path must be repository-relative: %q", file)
	}
	if r.Line < 1 || r.Column < 0 || r.EndLine < 0 || r.EndColumn < 0 || (r.EndLine > 0 && r.EndLine < r.Line) || (r.EndColumn > 0 && (r.Column == 0 || r.EndLine == 0)) || (r.EndLine == r.Line && r.EndColumn > 0 && r.EndColumn < r.Column) {
		return nil, fmt.Errorf("invalid source region")
	}
	region := schema.NewRegion().WithStartLine(r.Line)
	if r.Column > 0 {
		region.WithStartColumn(r.Column)
	}
	if r.EndLine > 0 {
		region.WithEndLine(r.EndLine)
	}
	if r.EndColumn > 0 {
		region.WithEndColumn(r.EndColumn)
	}
	uri := (&url.URL{Path: file}).EscapedPath()
	return schema.NewLocationWithPhysicalLocation(schema.NewPhysicalLocation().WithArtifactLocation(schema.NewArtifactLocation().WithURI(uri).WithURIBaseID("%SRCROOT%")).WithRegion(region)), nil
}

// Write propagates output failures. Schema validation is explicit so consumers
// can choose when to pay its cost, independently of serializing a report.
func Write(w io.Writer, report *schema.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// Position converts a byte offset to one-based SARIF UTF-16 coordinates.
// It rejects offsets inside UTF-8 characters and CRLF terminators.
func Position(source []byte, offset int) (line, column int, err error) {
	index, err := NewSource(source)
	if err != nil {
		return 0, 0, err
	}
	return index.At(offset)
}
