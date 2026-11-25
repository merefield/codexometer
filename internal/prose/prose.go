// Package prose projects assistant Markdown into text, tables and typed thread references.
// It never renders HTML or ANSI, and never replaces the original message.
package prose

import (
	"bytes"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	table "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type ProseRun struct {
	Text     string `json:"text"`
	Bold     bool   `json:"bold,omitempty"`
	Italic   bool   `json:"italic,omitempty"`
	Code     bool   `json:"code,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
}

type ProseBlock struct {
	Runs  []ProseRun     `json:"runs,omitempty"`
	Rows  [][][]ProseRun `json:"rows,omitempty"` // header first
	Align []string       `json:"align,omitempty"`
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.Table))

// Cache keeps parsing off the repaint path. Each dashboard holds only a small
// in-memory window, shared by its render and layout passes.
type Cache struct {
	mu      sync.Mutex
	entries [32]struct {
		source string
		doc    []ProseBlock
	}
	next int
}

func (c *Cache) Get(s string) []ProseBlock {
	if c == nil {
		return Parse(s)
	}
	if !candidate(s) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.entries {
		if c.entries[i].source == s {
			return c.entries[i].doc
		}
	}
	e := &c.entries[c.next]
	c.next = (c.next + 1) % len(c.entries)
	e.source, e.doc = s, Parse(s)
	return e.doc
}

func candidate(s string) bool {
	return len(s) <= 32*1024 && (strings.ContainsAny(s, "*_|`") || strings.Contains(s, "codex://threads/"))
}

// ThreadID permits exactly the native Codex thread-link shape. The result is
// an internal identifier, never an OS link or a fetchable URL.
func ThreadID(destination string) string {
	id, ok := strings.CutPrefix(destination, "codex://threads/")
	if !ok || len(id) != 36 {
		return ""
	}
	if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
		return id
	}
	return ""
}

func HasThreadLinks(blocks []ProseBlock) bool {
	for _, block := range blocks {
		if linkedRuns(block.Runs) {
			return true
		}
		for _, row := range block.Rows {
			for _, cell := range row {
				if linkedRuns(cell) {
					return true
				}
			}
		}
	}
	return false
}

func linkedRuns(runs []ProseRun) bool {
	for _, run := range runs {
		if run.ThreadID != "" {
			return true
		}
	}
	return false
}

// Parse returns nil for plain text or unsupported layouts. Keep untouched
// portions verbatim, including fenced code, list markers and link destinations.
func Parse(source string) []ProseBlock {
	if !candidate(source) {
		return nil
	}
	// GFM pads short rows up to the header width. Bound the prospective
	// rectangular expansion before constructing the parser's table AST.
	if strings.Contains(source, "|") {
		rows, cols := 0, 0
		for line := range strings.SplitSeq(source, "\n") {
			if strings.TrimSpace(line) == "" {
				rows, cols = 0, 0
				continue
			}
			rows++
			cols = max(cols, strings.Count(line, "|")+1)
			if cols > 64 || rows*cols > 4096 {
				return nil
			}
		}
	}
	src := []byte(source)
	root := markdown.Parser().Parse(text.NewReader(src))
	var blocks []ProseBlock
	var runs []ProseRun
	pos := 0
	plain := func(s string) {
		if s != "" {
			runs = append(runs, ProseRun{Text: s})
		}
	}
	flush := func() {
		if len(runs) > 0 {
			blocks = append(blocks, ProseBlock{Runs: runs})
			runs = nil
		}
	}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *table.Table:
			if n.Parent() != root {
				return ast.WalkSkipChildren, nil
			}
			first := v.FirstChild().Pos()
			if first < pos {
				return ast.WalkSkipChildren, nil
			}
			lineEnd := func(i int) int {
				if j := strings.IndexByte(source[i:], '\n'); j >= 0 {
					return i + j + 1
				}
				return len(source)
			}
			last := v.LastChild().Pos()
			if last == first {
				last = lineEnd(first)
			} // header-only table: consume the delimiter
			b := ProseBlock{}
			for _, a := range v.Alignments {
				b.Align = append(b.Align, a.String())
			}
			for row := v.FirstChild(); row != nil; row = row.NextSibling() {
				cells := make([][]ProseRun, 0, len(b.Align))
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					var r []ProseRun
					if lines := cell.Lines(); lines.Len() > 0 {
						seg := lines.At(0)
						r, _ = inlineRuns(cell, src, seg.Stop, ProseRun{})
						for i := range r {
							// Goldmark's table transformer already unescaped code spans.
							if !r[i].Code {
								r[i].Text = strings.ReplaceAll(r[i].Text, `\|`, "|")
							}
						}
					}
					cells = append(cells, r)
				}
				b.Rows = append(b.Rows, cells)
			}
			plain(strings.TrimSuffix(strings.TrimSuffix(source[pos:first], "\n"), "\r"))
			flush()
			blocks = append(blocks, b)
			pos = lineEnd(last)
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
			lines := n.Lines()
			if lines.Len() == 0 {
				return ast.WalkSkipChildren, nil
			}
			start, end := lines.At(0).Start, lines.At(lines.Len()-1).Stop
			if start < pos {
				return ast.WalkSkipChildren, nil
			}
			// Don't remove blockquote/list continuation prefixes from source.
			for i := 1; i < lines.Len(); i++ {
				if lines.At(i).Start != lines.At(i-1).Stop {
					return ast.WalkSkipChildren, nil
				}
			}
			end = start + len(bytes.TrimRight(src[start:end], "\r\n"))
			if r, rich := inlineRuns(n, src, end, ProseRun{}); rich {
				plain(source[pos:start])
				runs = append(runs, r...)
				pos = end
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if len(blocks) == 0 && len(runs) == 0 {
		return nil
	}
	plain(source[pos:])
	flush()
	return blocks
}

// Goldmark identifies the inlines (including nested delimiter runs). For
// other inline syntax keep the complete source slice, not just its label.
func inlineRuns(parent ast.Node, src []byte, end int, style ProseRun) (out []ProseRun, rich bool) {
	add := func(s string) {
		if s != "" {
			r := style
			r.Text = s
			out = append(out, r)
		}
	}
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		stop := end
		if next := n.NextSibling(); next != nil {
			stop = next.Pos()
		}
		literal := func() {
			if p := n.Pos(); p >= 0 && p <= stop && stop <= len(src) {
				add(string(src[p:stop]))
			}
		}
		switch v := n.(type) {
		case *ast.Text:
			add(string(v.Segment.Value(src)))
			if v.SoftLineBreak() || v.HardLineBreak() {
				add("\n")
			}
		case *ast.String:
			add(string(v.Value))
		case *ast.Emphasis:
			next := style
			if v.Level == 2 {
				next.Bold = true
			} else {
				next.Italic = true
			}
			s, _ := inlineRuns(v, src, stop-v.Level, next)
			out = append(out, s...)
			rich = true
		case *ast.CodeSpan:
			var code strings.Builder
			for c := v.FirstChild(); c != nil; c = c.NextSibling() {
				code.Write(c.(*ast.Text).Segment.Value(src))
			}
			r := style
			r.Code = true
			r.Text = strings.ReplaceAll(code.String(), "\n", " ")
			out = append(out, r)
			rich = true
		case *ast.Link:
			if id := ThreadID(string(v.Destination)); id != "" && strings.TrimSpace(string(v.Text(src))) != "" {
				r := style
				r.Text, r.ThreadID = string(v.Text(src)), id
				out, rich = append(out, r), true
			} else {
				literal()
			}
		default:
			literal()
		}
	}
	return
}
