// Package markdown renders Markdown to sanitized HTML with syntax
// highlighting, reading time and a table of contents.
package markdown

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type TOCEntry struct {
	Level int
	ID    string
	Title string
}

type Result struct {
	HTML        template.HTML
	TOC         []TOCEntry
	ReadingTime int // minutes, at least 1
}

const wordsPerMinute = 220

// styleAttr only admits the simple declarations chroma emits.
var styleAttr = regexp.MustCompile(`^(?:\s*(?:color|background-color|font-weight|font-style|text-decoration)\s*:\s*[#\w\s-]+;?)+$`)

type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

func New() *Renderer {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("code", "pre", "span", "div")
	p.AllowAttrs("style").Matching(styleAttr).OnElements("span", "pre")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	return &Renderer{
		md: goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				highlighting.NewHighlighting(highlighting.WithStyle("github")),
			),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		),
		policy: p,
	}
}

func (r *Renderer) Render(src string) (Result, error) {
	b := []byte(src)
	doc := r.md.Parser().Parse(text.NewReader(b))

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, b, doc); err != nil {
		return Result{}, err
	}
	return Result{
		HTML:        template.HTML(r.policy.SanitizeBytes(buf.Bytes())), //nolint:gosec // sanitized
		TOC:         toc(doc, b),
		ReadingTime: readingTime(src),
	}, nil
}

func toc(doc ast.Node, src []byte) []TOCEntry {
	var out []TOCEntry
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok || h.Level < 2 || h.Level > 3 {
			return ast.WalkContinue, nil
		}
		id, _ := h.AttributeString("id")
		idb, _ := id.([]byte)
		out = append(out, TOCEntry{Level: h.Level, ID: string(idb), Title: string(h.Lines().Value(src))})
		return ast.WalkContinue, nil
	})
	return out
}

func readingTime(s string) int {
	words := len(strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
	if m := (words + wordsPerMinute - 1) / wordsPerMinute; m > 1 {
		return m
	}
	return 1
}
