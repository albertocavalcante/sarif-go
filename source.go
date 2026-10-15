package sarif

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Source indexes an immutable snapshot. It never reads files and is safe to
// share between adapters. Offsets always refer to the original UTF-8 bytes.
type Source struct {
	text         string
	starts, ends []int
}

func NewSource(content []byte) (*Source, error) {
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("source is not valid UTF-8")
	}
	s := &Source{text: string(content), starts: []int{0}}
	for i := 0; i < len(content); i++ {
		if content[i] == '\r' || content[i] == '\n' {
			s.ends = append(s.ends, i)
			if content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n' {
				i++
			}
			s.starts = append(s.starts, i+1)
		}
	}
	s.ends = append(s.ends, len(content))
	return s, nil
}

func (s *Source) At(offset int) (line, column int, err error) {
	if offset < 0 || offset > len(s.text) || (offset < len(s.text) && !utf8.RuneStart(s.text[offset])) || (offset > 0 && offset < len(s.text) && s.text[offset-1] == '\r' && s.text[offset] == '\n') {
		return 0, 0, fmt.Errorf("invalid source byte offset %d", offset)
	}
	i := sort.Search(len(s.starts), func(i int) bool { return s.starts[i] > offset }) - 1
	column = 1
	for _, r := range s.text[s.starts[i]:offset] {
		column++
		if r > 0xffff {
			column++
		}
	}
	return i + 1, column, nil
}

func (s *Source) Region(start, end int) (Region, error) {
	if end < start {
		return Region{}, fmt.Errorf("source span runs backwards")
	}
	l, c, err := s.At(start)
	if err != nil {
		return Region{}, err
	}
	el, ec, err := s.At(end)
	if err != nil {
		return Region{}, err
	}
	return Region{l, c, el, ec}, nil
}

// RuneRegion adapts one-based Unicode code-point positions, including tabs as
// single characters. Zero end positions mean the source extent is unknown.
func (s *Source) RuneRegion(line, column, endLine, endColumn int) (Region, error) {
	convert := func(line, col int) (int, error) {
		if line < 1 || line > len(s.starts) || col < 1 {
			return 0, fmt.Errorf("invalid source position %d:%d", line, col)
		}
		text := s.text[s.starts[line-1]:s.ends[line-1]]
		units, count := 1, 1
		for _, r := range text {
			if count == col {
				return units, nil
			}
			count++
			units++
			if r > 0xffff {
				units++
			}
		}
		if count == col {
			return units, nil
		}
		return 0, fmt.Errorf("column %d outside line %d", col, line)
	}
	c, err := convert(line, column)
	if err != nil {
		return Region{}, err
	}
	r := Region{Line: line, Column: c}
	if endLine > 0 && endColumn > 0 {
		ec, err := convert(endLine, endColumn)
		if err != nil {
			return Region{}, err
		}
		r.EndLine = endLine
		r.EndColumn = ec
	}
	return r, nil
}

// RelativePath resolves a native path against an explicit checkout root.
// Callers choose the root; this function never consults observed working state.
// Relative file paths are interpreted relative to root. CLI adapters reading
// paths relative to a working directory must resolve those paths first.
func RelativePath(root, file string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("checkout root must be absolute")
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("source %q is outside checkout %q", file, root)
	}
	return rel, nil
}

func RootURI(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("checkout root must be absolute")
	}
	p := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(root)), "/") + "/"
	// URL file paths on Windows require a slash before the drive letter.
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String(), nil
}
