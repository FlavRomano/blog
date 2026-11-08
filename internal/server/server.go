// Package server wires the HTTP handler stack.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/markdown"
)

type Deps struct {
	DB  *sql.DB
	Log *slog.Logger
	Cfg config.Config
}

type Server struct {
	db     *sql.DB
	q      *sqlc.Queries
	log    *slog.Logger
	cfg    config.Config
	md     *markdown.Renderer
	cache  *renderCache
	pages  map[string]*pageTemplate
	site   siteInfo
	assets *assets
}

func New(d Deps) (http.Handler, error) {
	a, err := loadAssets()
	if err != nil {
		return nil, err
	}
	pages, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	s := &Server{
		db: d.DB, q: sqlc.New(d.DB), log: d.Log, cfg: d.Cfg,
		md: markdown.New(), cache: newRenderCache(256),
		pages: pages, assets: a,
		site: siteInfo{
			Name:         d.Cfg.SiteName,
			Author:       d.Cfg.AuthorName,
			Description:  d.Cfg.SiteDescription,
			BaseURL:      d.Cfg.BaseURL,
			BeaconToken:  d.Cfg.CFBeaconToken,
			AssetVersion: a.version,
			Nav:          []navItem{{"/", "Posts"}, {"/tags", "Tags"}, {"/projects", "Projects"}, {"/about", "About"}},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /rss.xml", s.rss)
	mux.HandleFunc("GET /atom.xml", s.atom)
	mux.Handle("GET /static/", s.assets.handler())

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /posts/{slug}", s.post)
	mux.HandleFunc("GET /tags", s.tags)
	mux.HandleFunc("GET /tags/{slug}", s.tag)
	mux.HandleFunc("GET /about", s.static("about.html", "About", "/about", "About me and what this site is."))
	mux.HandleFunc("GET /projects", s.static("projects.html", "Projects", "/projects", "Things I have built."))
	mux.HandleFunc("GET /privacy", s.static("privacy.html", "Privacy", "/privacy", "What data this site collects."))
	mux.HandleFunc("/", s.notFound)

	return chain(mux, s.recoverer, s.logRequests, securityHeaders), nil
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		s.log.Error("healthz: db ping failed", "err", err)
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\n\nSitemap: %s/sitemap.xml\n", s.cfg.BaseURL)
}
