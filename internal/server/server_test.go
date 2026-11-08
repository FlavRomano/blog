package server_test

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/server"
)

func newEnv(t *testing.T) (http.Handler, *sqlc.Queries) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h, err := server.New(server.Deps{
		DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg: config.Config{BaseURL: "https://example.com", SiteName: "Test Site", AuthorName: "Tester", SiteDescription: "desc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, sqlc.New(db)
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", path, nil))
	return rec
}

func addPost(t *testing.T, q *sqlc.Queries, slug, status, at string, tags ...string) {
	t.Helper()
	p, err := q.CreatePost(context.Background(), sqlc.CreatePostParams{
		Slug: slug, Title: "Title " + slug, Summary: "Summary " + slug,
		BodyMd: "## First\n\nhello **world** <script>x</script>\n\n## Second\n\n```go\nx := 1\n```\n",
		Status: status, PublishedAt: sql.NullString{String: at, Valid: at != ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range tags {
		tg, err := q.UpsertTag(context.Background(), sqlc.UpsertTagParams{Slug: strings.ToLower(name), Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if err := q.AddPostTag(context.Background(), sqlc.AddPostTagParams{PostID: p.ID, TagID: tg.ID}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthzAndHeaders(t *testing.T) {
	h, _ := newEnv(t)
	rec := get(t, h, "/healthz")
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("security headers missing: %v", rec.Header())
	}
}

func TestRobotsHasSitemap(t *testing.T) {
	h, _ := newEnv(t)
	if body := get(t, h, "/robots.txt").Body.String(); !strings.Contains(body, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("robots: %q", body)
	}
}

func TestHomeListsOnlyPublishedAndPaginates(t *testing.T) {
	h, q := newEnv(t)
	for i := 1; i <= 12; i++ {
		addPost(t, q, fmt.Sprintf("p%02d", i), "published", fmt.Sprintf("2024-01-%02dT00:00:00Z", i))
	}
	addPost(t, q, "draft", "draft", "")
	addPost(t, q, "later", "scheduled", "2999-01-01T00:00:00Z")

	rec := get(t, h, "/")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Title p12") || strings.Contains(body, "Title p01") {
		t.Fatalf("page 1 wrong: %d", rec.Code)
	}
	if strings.Contains(body, "Title draft") || strings.Contains(body, "Title later") {
		t.Error("unpublished post leaked")
	}
	if !strings.Contains(body, `href="/?page=2"`) {
		t.Error("missing next link")
	}
	if b2 := get(t, h, "/?page=2").Body.String(); !strings.Contains(b2, "Title p01") || !strings.Contains(b2, `rel="prev"`) {
		t.Error("page 2 wrong")
	}
}

func TestPostPage(t *testing.T) {
	h, q := newEnv(t)
	addPost(t, q, "hello", "published", "2024-03-05T10:00:00Z", "Go")
	addPost(t, q, "secret", "draft", "")

	rec := get(t, h, "/posts/hello")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	for _, want := range []string{
		"<title>Title hello · Test Site</title>",
		`<link rel="canonical" href="https://example.com/posts/hello">`,
		`og:type" content="article"`,
		"<strong>world</strong>", `href="#first"`, "5 March 2024", "min read", `href="/tags/go"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "<script>x") {
		t.Error("unsanitized HTML")
	}
	if get(t, h, "/posts/secret").Code != 404 || get(t, h, "/posts/none").Code != 404 {
		t.Error("expected 404 for draft/missing")
	}
}

func TestTags(t *testing.T) {
	h, q := newEnv(t)
	addPost(t, q, "a", "published", "2024-01-01T00:00:00Z", "Go")
	addPost(t, q, "b", "draft", "", "Rust")

	if b := get(t, h, "/tags").Body.String(); !strings.Contains(b, "#Go") || strings.Contains(b, "#Rust") {
		t.Errorf("tags index: %s", b)
	}
	if b := get(t, h, "/tags/go").Body.String(); !strings.Contains(b, "Title a") {
		t.Error("tag page missing post")
	}
	if get(t, h, "/tags/nope").Code != 404 {
		t.Error("expected 404")
	}
}

func TestStaticPagesAndNotFound(t *testing.T) {
	h, _ := newEnv(t)
	for _, p := range []string{"/about", "/projects", "/privacy", "/static/app.css", "/static/theme.js", "/static/chroma.css"} {
		if c := get(t, h, p).Code; c != 200 {
			t.Errorf("%s: %d", p, c)
		}
	}
	rec := get(t, h, "/nope")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "noindex") {
		t.Errorf("404 page: %d", rec.Code)
	}
}

func TestFeedsAndSitemapAreValidXML(t *testing.T) {
	h, q := newEnv(t)
	addPost(t, q, "a", "published", "2024-01-01T00:00:00Z", "Go")
	addPost(t, q, "d", "draft", "")

	for path, ctype := range map[string]string{"/rss.xml": "application/rss+xml", "/atom.xml": "application/atom+xml", "/sitemap.xml": "application/xml"} {
		rec := get(t, h, path)
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		dec := xml.NewDecoder(strings.NewReader(rec.Body.String()))
		for {
			if _, err := dec.Token(); err != nil {
				if !errors.Is(err, io.EOF) {
					t.Errorf("%s invalid xml: %v", path, err)
				}
				break
			}
		}
		body := rec.Body.String()
		if !strings.Contains(body, "https://example.com/posts/a") || strings.Contains(body, "/posts/d") {
			t.Errorf("%s content wrong", path)
		}
	}
}
