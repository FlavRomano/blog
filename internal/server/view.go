package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/markdown"
	"github.com/FlavRomano/blog/web"
)

type navItem struct{ Href, Label string }

type siteInfo struct {
	Name, Author, Description, BaseURL, BeaconToken, AssetVersion string
	Nav                                                           []navItem
}

type pageData struct {
	Site          siteInfo
	Title         string // empty means site name only
	Description   string
	Path          string // canonical path, also used for nav highlighting
	Nav           string
	OGType        string
	PublishedTime string
	Tags          []sqlc.Tag
	NoIndex       bool
	Year          int
	Data          any
}

func (p pageData) FullTitle() string {
	if p.Title == "" {
		return p.Site.Name
	}
	return p.Title + " · " + p.Site.Name
}

func (p pageData) Canonical() string { return p.Site.BaseURL + p.Path }

type pageTemplate struct{ t *template.Template }

func parseTemplates() (map[string]*pageTemplate, error) {
	files, err := fs.Glob(web.FS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	out := map[string]*pageTemplate{}
	for _, f := range files {
		name := f[len("templates/"):]
		if name == "base.html" || name == "partials.html" {
			continue
		}
		t, err := template.ParseFS(web.FS, "templates/base.html", "templates/partials.html", f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = &pageTemplate{t}
	}
	return out, nil
}

func (s *Server) render(w http.ResponseWriter, status int, tmpl string, p pageData) {
	p.Site = s.site
	p.Year = time.Now().Year()
	if p.Nav == "" {
		p.Nav = p.Path
	}
	if p.Description == "" {
		p.Description = s.site.Description
	}
	if p.OGType == "" {
		p.OGType = "website"
	}
	var buf bytes.Buffer
	if err := s.pages[tmpl].t.ExecuteTemplate(&buf, "base", p); err != nil {
		s.log.Error("render", "tmpl", tmpl, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) notFound(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusNotFound, "notfound.html", pageData{Title: "Not found", Path: "/404", NoIndex: true})
}

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// renderCache memoises rendered Markdown keyed by post id and update time.
type renderCache struct {
	mu  sync.Mutex
	max int
	m   map[string]markdown.Result
}

func newRenderCache(max int) *renderCache {
	return &renderCache{max: max, m: map[string]markdown.Result{}}
}

func (c *renderCache) get(key string, render func() (markdown.Result, error)) (markdown.Result, error) {
	c.mu.Lock()
	if r, ok := c.m[key]; ok {
		c.mu.Unlock()
		return r, nil
	}
	c.mu.Unlock()
	r, err := render()
	if err != nil {
		return r, err
	}
	c.mu.Lock()
	if len(c.m) >= c.max {
		clear(c.m)
	}
	c.m[key] = r
	c.mu.Unlock()
	return r, nil
}

// postView is a post prepared for templates.
type postView struct {
	Slug, Title, Summary string
	Date                 time.Time
	DateISO, DateHuman   string
	ReadingTime          int
	Tags                 []sqlc.Tag
	Updated              time.Time
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (s *Server) toView(r *http.Request, p sqlc.Post) (postView, error) {
	tags, err := s.q.ListPostTags(r.Context(), p.ID)
	if err != nil {
		return postView{}, err
	}
	d := parseTime(p.PublishedAt.String)
	return postView{
		Slug: p.Slug, Title: p.Title, Summary: p.Summary,
		Date: d, DateISO: d.Format(time.RFC3339), DateHuman: d.Format("2 January 2006"),
		ReadingTime: markdown.ReadingTime(p.BodyMd), Tags: tags,
		Updated: parseTime(p.UpdatedAt),
	}, nil
}

func (s *Server) renderBody(p sqlc.Post) (markdown.Result, error) {
	return s.cache.get(fmt.Sprintf("%d@%s", p.ID, p.UpdatedAt), func() (markdown.Result, error) {
		return s.md.Render(p.BodyMd)
	})
}
