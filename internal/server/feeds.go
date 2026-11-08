package server

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/FlavRomano/blog/internal/database/sqlc"
)

const feedLimit = 20

type feedItem struct {
	post sqlc.Post
	view postView
	html string
}

func (s *Server) feedItems(r *http.Request) ([]feedItem, error) {
	posts, err := s.q.ListPublishedPosts(r.Context(), sqlc.ListPublishedPostsParams{Limit: feedLimit})
	if err != nil {
		return nil, err
	}
	items := make([]feedItem, 0, len(posts))
	for _, p := range posts {
		v, err := s.toView(r, p)
		if err != nil {
			return nil, err
		}
		res, err := s.renderBody(p)
		if err != nil {
			return nil, err
		}
		items = append(items, feedItem{p, v, strings.ReplaceAll(string(res.HTML), `src="/uploads/`, `src="`+s.site.BaseURL+`/uploads/`)})
	}
	return items, nil
}

func (s *Server) writeXML(w http.ResponseWriter, ctype string, v any) {
	w.Header().Set("Content-Type", ctype+"; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		s.log.Error("encode xml", "err", err)
	}
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Content string     `xml:"xmlns:content,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Self          atomLink  `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description"`
	Content     string   `xml:"content:encoded"`
	Categories  []string `xml:"category"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

func (s *Server) rss(w http.ResponseWriter, r *http.Request) {
	items, err := s.feedItems(r)
	if err != nil {
		s.serverError(w, "feed items", err)
		return
	}
	ch := rssChannel{
		Title: s.site.Name, Link: s.site.BaseURL + "/", Description: s.site.Description, Language: "en",
		Self: atomLink{Href: s.site.BaseURL + "/rss.xml", Rel: "self", Type: "application/rss+xml"},
	}
	for i, it := range items {
		if i == 0 {
			ch.LastBuildDate = it.view.Date.Format(time.RFC1123Z)
		}
		url := s.site.BaseURL + "/posts/" + it.post.Slug
		ri := rssItem{
			Title: it.post.Title, Link: url, GUID: url, PubDate: it.view.Date.Format(time.RFC1123Z),
			Description: it.post.Summary, Content: it.html,
		}
		for _, t := range it.view.Tags {
			ri.Categories = append(ri.Categories, t.Name)
		}
		ch.Items = append(ch.Items, ri)
	}
	s.writeXML(w, "application/rss+xml", rssFeed{
		Version: "2.0", Content: "http://purl.org/rss/1.0/modules/content/", Atom: "http://www.w3.org/2005/Atom", Channel: ch,
	})
}

type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Author   atomAuthor  `xml:"author"`
	Entries  []atomEntry `xml:"entry"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomText struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomEntry struct {
	Title     string         `xml:"title"`
	ID        string         `xml:"id"`
	Link      atomLink       `xml:"link"`
	Published string         `xml:"published"`
	Updated   string         `xml:"updated"`
	Summary   string         `xml:"summary,omitempty"`
	Content   atomText       `xml:"content"`
	Category  []atomCategory `xml:"category"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

func (s *Server) atom(w http.ResponseWriter, r *http.Request) {
	items, err := s.feedItems(r)
	if err != nil {
		s.serverError(w, "feed items", err)
		return
	}
	f := atomFeed{
		Title: s.site.Name, Subtitle: s.site.Description, ID: s.site.BaseURL + "/",
		Links: []atomLink{
			{Href: s.site.BaseURL + "/atom.xml", Rel: "self", Type: "application/atom+xml"},
			{Href: s.site.BaseURL + "/", Rel: "alternate", Type: "text/html"},
		},
		Author:  atomAuthor{s.site.Author},
		Updated: time.Unix(0, 0).UTC().Format(time.RFC3339),
	}
	for i, it := range items {
		url := s.site.BaseURL + "/posts/" + it.post.Slug
		upd := it.view.Updated
		if upd.IsZero() {
			upd = it.view.Date
		}
		if i == 0 {
			f.Updated = upd.Format(time.RFC3339)
		}
		e := atomEntry{
			Title: it.post.Title, ID: url, Link: atomLink{Href: url, Rel: "alternate", Type: "text/html"},
			Published: it.view.Date.Format(time.RFC3339), Updated: upd.Format(time.RFC3339),
			Summary: it.post.Summary, Content: atomText{Type: "html", Body: it.html},
		}
		for _, t := range it.view.Tags {
			e.Category = append(e.Category, atomCategory{t.Name})
		}
		f.Entries = append(f.Entries, e)
	}
	s.writeXML(w, "application/atom+xml", f)
}

type urlSet struct {
	XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	base := s.site.BaseURL
	set := urlSet{}
	for _, p := range []string{"/", "/tags", "/projects", "/about", "/privacy"} {
		set.URLs = append(set.URLs, sitemapURL{Loc: base + p})
	}
	refs, err := s.q.ListPublishedPostRefs(r.Context())
	if err != nil {
		s.serverError(w, "sitemap posts", err)
		return
	}
	for _, p := range refs {
		set.URLs = append(set.URLs, sitemapURL{Loc: base + "/posts/" + p.Slug, LastMod: parseTime(p.UpdatedAt).Format(time.RFC3339)})
	}
	tags, err := s.q.ListTagsWithCounts(r.Context())
	if err != nil {
		s.serverError(w, "sitemap tags", err)
		return
	}
	for _, t := range tags {
		set.URLs = append(set.URLs, sitemapURL{Loc: base + "/tags/" + t.Slug})
	}
	s.writeXML(w, "application/xml", set)
}
