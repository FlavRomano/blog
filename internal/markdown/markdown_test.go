package markdown

import (
	"strings"
	"testing"
)

func TestRenderSanitizes(t *testing.T) {
	res, err := New().Render("hi <script>alert(1)</script> [x](javascript:alert(1)) <img src=x onerror=alert(1)>")
	if err != nil {
		t.Fatal(err)
	}
	h := string(res.HTML)
	for _, bad := range []string{"<script", "javascript:", "onerror"} {
		if strings.Contains(h, bad) {
			t.Errorf("output contains %q: %s", bad, h)
		}
	}
}

func TestRenderTOCAndHighlight(t *testing.T) {
	res, err := New().Render("# T\n\n## Intro\n\ntext\n\n### Deep\n\n```go\nfunc main() {}\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TOC) != 2 || res.TOC[0].ID != "intro" || res.TOC[1].Level != 3 {
		t.Fatalf("toc: %+v", res.TOC)
	}
	if !strings.Contains(string(res.HTML), `id="intro"`) {
		t.Errorf("heading id stripped: %s", res.HTML)
	}
	if !strings.Contains(string(res.HTML), "<pre") || !strings.Contains(string(res.HTML), `class="chroma"`) {
		t.Errorf("highlighting missing: %s", res.HTML)
	}
}

func TestReadingTime(t *testing.T) {
	if got := ReadingTime("a b c"); got != 1 {
		t.Errorf("got %d", got)
	}
	if got := ReadingTime(strings.Repeat("word ", 661)); got != 4 {
		t.Errorf("got %d", got)
	}
}
