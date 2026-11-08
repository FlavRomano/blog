package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/FlavRomano/blog/internal/auth"
)

type ctxKey struct{}

func sessionFrom(r *http.Request) auth.Session {
	s, _ := r.Context().Value(ctxKey{}).(auth.Session)
	return s
}

func (s *Server) secureCookies() bool { return strings.HasPrefix(s.cfg.BaseURL, "https://") }

func (s *Server) cookieName() string {
	if s.secureCookies() {
		return "__Host-session"
	}
	return "session"
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", s.noStore(s.loginForm))
	mux.HandleFunc("POST /admin/login", s.noStore(s.login))

	guard := func(h http.HandlerFunc) http.HandlerFunc { return s.noStore(s.requireAdmin(h)) }
	mux.HandleFunc("GET /admin", guard(s.dashboard))
	mux.HandleFunc("GET /admin/{$}", guard(s.dashboard))
	mux.HandleFunc("POST /admin/logout", guard(s.logout))
	mux.HandleFunc("GET /admin/posts/new", guard(s.postNew))
	mux.HandleFunc("POST /admin/posts", guard(s.postCreate))
	mux.HandleFunc("GET /admin/posts/{id}", guard(s.postEdit))
	mux.HandleFunc("POST /admin/posts/{id}", guard(s.postUpdate))
	mux.HandleFunc("POST /admin/posts/{id}/delete", guard(s.postDelete))
	mux.HandleFunc("POST /admin/preview", guard(s.preview))
	mux.HandleFunc("POST /admin/images", guard(s.imageUpload))
	mux.HandleFunc("GET /admin/comments", guard(s.commentsAdmin))
	mux.HandleFunc("POST /admin/comments/{id}/delete", guard(s.commentDelete))
	mux.HandleFunc("POST /admin/comments/{id}/ban", guard(s.commentBan))
	mux.HandleFunc("POST /admin/bans/{hash}/delete", guard(s.banDelete))
}

func (s *Server) noStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var token string
		if c, err := r.Cookie(s.cookieName()); err == nil {
			token = c.Value
		}
		sess, err := s.auth.Authenticate(r.Context(), token)
		if errors.Is(err, auth.ErrNoSession) {
			if r.Header.Get("HX-Request") != "" {
				w.Header().Set("HX-Redirect", "/admin/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if err != nil {
			s.serverError(w, "authenticate", err)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
			got := r.Header.Get("X-CSRF-Token")
			if got == "" {
				got = r.PostFormValue("csrf")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRF)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	}
}

// adminPage builds pageData for an authenticated admin view.
func (s *Server) adminPage(r *http.Request, title string, data any) pageData {
	unseen, err := s.q.CountUnseenComments(r.Context())
	if err != nil {
		s.log.Error("count unseen comments", "err", err)
	}
	return pageData{Title: title, Path: r.URL.Path, NoIndex: true, LoggedIn: true, CSRF: sessionFrom(r).CSRF, Unseen: int(unseen), Data: data}
}

type loginData struct{ Error string }

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if _, err := s.auth.Authenticate(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
	}
	s.render(w, http.StatusOK, "admin_login.html", pageData{Title: "Sign in", Path: "/admin/login", NoIndex: true, Data: loginData{}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sess, err := s.auth.Login(r.Context(), s.ip.Hash(r), r.PostFormValue("password"), r.PostFormValue("code"))
	fail := func(status int, msg string) {
		s.render(w, status, "admin_login.html", pageData{Title: "Sign in", Path: "/admin/login", NoIndex: true, Data: loginData{Error: msg}})
	}
	switch {
	case errors.Is(err, auth.ErrLockedOut):
		w.Header().Set("Retry-After", "900")
		fail(http.StatusTooManyRequests, "Too many failed attempts. Try again in 15 minutes.")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.log.Warn("admin login failed", "ip_hash", s.ip.Hash(r))
		fail(http.StatusUnauthorized, "Invalid credentials.")
		return
	case err != nil:
		s.serverError(w, "login", err)
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is enabled whenever the base URL is https
		Name: s.cookieName(), Value: sess.Token, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	s.log.Info("admin login")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(r.Context(), sessionFrom(r).Token); err != nil {
		s.log.Error("logout", "err", err)
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is enabled whenever the base URL is https
		Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

type adminRow struct {
	ID            int64
	Title, Slug   string
	Status, Dated string
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	posts, err := s.q.ListAllPosts(r.Context())
	if err != nil {
		s.serverError(w, "list all posts", err)
		return
	}
	rows := make([]adminRow, len(posts))
	for i, p := range posts {
		rows[i] = adminRow{ID: p.ID, Title: p.Title, Slug: p.Slug, Status: p.Status, Dated: s.localTime(p.PublishedAt)}
	}
	s.render(w, http.StatusOK, "admin_dashboard.html", s.adminPage(r, "Posts", rows))
}

func (s *Server) localTime(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return parseTime(v.String).In(s.loc).Format("2006-01-02 15:04")
}
