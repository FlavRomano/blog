package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/markdown"
)

const (
	maxCommentDepth = 4
	maxNameLen      = 50
	minBodyLen      = 2
	maxCommentLen   = 2000
	maxLinks        = 2
	maxCommentForm  = 16 << 10

	perIPShort      = 3
	shortWindow     = 10 * time.Minute
	perIPDay        = 20
	dayWindow       = 24 * time.Hour
	globalPerHour   = 120
	notifyExcerptLn = 140
)

var linkRe = regexp.MustCompile(`(?i)https?://|www\.`)

// commentNode is one comment prepared for the template, with its replies.
type commentNode struct {
	ID       int64
	Author   string
	Body     string
	Deleted  bool
	DateISO  string
	DateText string
	Children []*commentNode
}

type commentForm struct {
	Name, Body string
	ParentID   int64
	ReplyTo    string // author of the parent, shown above the form
	Error      string
}

type postPage struct {
	Post            postView
	HTML            template.HTML
	TOC             []markdown.TOCEntry
	CommentsEnabled bool
	Comments        []*commentNode
	CommentCount    int
	Form            commentForm
	TurnstileKey    string
}

func (s *Server) commentsEnabled() bool { return s.ip != nil }

// buildComments assembles the reply tree. Rows arrive oldest first, so a
// parent always precedes its children.
func (s *Server) buildComments(rows []sqlc.ListCommentsByPostRow) (roots []*commentNode, live int) {
	byID := make(map[int64]*commentNode, len(rows))
	for _, c := range rows {
		n := &commentNode{ID: c.ID, Author: c.Author, Body: c.Body, Deleted: c.Deleted != 0}
		if t, err := time.Parse(auth.TimeFormat, c.CreatedAt); err == nil {
			n.DateISO = t.Format(time.RFC3339)
			n.DateText = t.In(s.loc).Format("2 Jan 2006, 15:04")
		}
		if !n.Deleted {
			live++
		}
		byID[c.ID] = n
		if parent, ok := byID[c.ParentID.Int64]; c.ParentID.Valid && ok {
			parent.Children = append(parent.Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	return roots, live
}

func (s *Server) postComments(w http.ResponseWriter, r *http.Request) {
	if !s.commentsEnabled() {
		s.notFound(w, r)
		return
	}
	p, err := s.q.GetPostBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && p.Status != "published") {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get post", err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCommentForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	form := commentForm{Name: cleanName(r.PostFormValue("name")), Body: normalizeBody(r.PostFormValue("body"))}
	form.ParentID, _ = strconv.ParseInt(r.PostFormValue("parent"), 10, 64)
	fail := func(status int, msg string) {
		form.Error = msg
		s.showPost(w, r, p, status, form)
	}

	// Bots fill the hidden field; pretend it worked and store nothing.
	if r.PostFormValue("website") != "" {
		http.Redirect(w, r, "/posts/"+p.Slug+"#comments", http.StatusSeeOther)
		return
	}
	hash := s.ip.Hash(r)
	banned, err := s.q.IsBanned(r.Context(), hash)
	if err != nil {
		s.serverError(w, "ban check", err)
		return
	}
	if banned {
		fail(http.StatusForbidden, "You can't post comments here.")
		return
	}
	if msg := s.validateComment(form); msg != "" {
		fail(http.StatusUnprocessableEntity, msg)
		return
	}
	if s.turnstile != nil {
		ok, err := s.turnstile.Verify(r.Context(), r.PostFormValue("cf-turnstile-response"), s.ip.IP(r).String())
		if err != nil {
			s.log.Error("turnstile", "err", err)
			fail(http.StatusServiceUnavailable, "Could not verify you are human right now. Please try again shortly.")
			return
		}
		if !ok {
			fail(http.StatusUnprocessableEntity, "Bot check failed. Please try again.")
			return
		}
	}

	id, err := s.insertComment(r.Context(), p.ID, hash, form)
	switch {
	case errors.Is(err, errRateLimited):
		w.Header().Set("Retry-After", "600")
		fail(http.StatusTooManyRequests, "You are commenting too fast. Please wait a few minutes.")
		return
	case errors.Is(err, errBadParent):
		form.ParentID = 0
		fail(http.StatusUnprocessableEntity, "The comment you replied to is no longer available.")
		return
	case err != nil:
		s.serverError(w, "insert comment", err)
		return
	}
	if s.notify != nil {
		s.notify("New comment on "+p.Title, form.Name+": "+excerpt(form.Body, notifyExcerptLn), s.cfg.BaseURL+"/admin/comments")
	}
	http.Redirect(w, r, "/posts/"+p.Slug+"#comment-"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

var (
	errRateLimited = errors.New("rate limited")
	errBadParent   = errors.New("bad parent")
)

// insertComment enforces rate limits and reply depth in one write transaction.
func (s *Server) insertComment(ctx context.Context, postID int64, hash string, f commentForm) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)

	now := time.Now().UTC()
	since := func(d time.Duration) string { return now.Add(-d).Format(auth.TimeFormat) }
	for _, l := range []struct {
		window time.Duration
		max    int64
	}{{shortWindow, perIPShort}, {dayWindow, perIPDay}} {
		n, err := q.CountCommentsByIPSince(ctx, sqlc.CountCommentsByIPSinceParams{IpHash: hash, CreatedAt: since(l.window)})
		if err != nil {
			return 0, err
		}
		if n >= l.max {
			return 0, errRateLimited
		}
	}
	n, err := q.CountCommentsSince(ctx, since(time.Hour))
	if err != nil {
		return 0, err
	}
	if n >= globalPerHour {
		return 0, errRateLimited
	}

	depth := int64(1)
	var parent sql.NullInt64
	if f.ParentID != 0 {
		pc, err := q.GetComment(ctx, f.ParentID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (pc.PostID != postID || pc.Deleted != 0)) {
			return 0, errBadParent
		}
		if err != nil {
			return 0, err
		}
		if pc.Depth >= maxCommentDepth {
			// Too deep: become a sibling of the deepest allowed comment.
			depth, parent = maxCommentDepth, pc.ParentID
		} else {
			depth, parent = pc.Depth+1, sql.NullInt64{Int64: pc.ID, Valid: true}
		}
	}
	id, err := q.InsertComment(ctx, sqlc.InsertCommentParams{
		PostID: postID, ParentID: parent, Depth: depth, Author: f.Name, Body: f.Body,
		IpHash: hash, CreatedAt: now.Format(auth.TimeFormat),
	})
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Server) validateComment(f commentForm) string {
	switch n := utf8.RuneCountInString(f.Name); {
	case n == 0:
		return "Please enter a name."
	case n > maxNameLen:
		return fmt.Sprintf("Name must be at most %d characters.", maxNameLen)
	case strings.EqualFold(f.Name, s.cfg.AuthorName):
		return "That name is reserved."
	}
	switch n := utf8.RuneCountInString(f.Body); {
	case n < minBodyLen:
		return "Comment is too short."
	case n > maxCommentLen:
		return fmt.Sprintf("Comment must be at most %d characters.", maxCommentLen)
	}
	if len(linkRe.FindAllString(f.Body, -1)) > maxLinks {
		return fmt.Sprintf("Comments may contain at most %d links.", maxLinks)
	}
	return ""
}

// cleanName trims, collapses whitespace and drops control characters.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// normalizeBody keeps plain text only: unified newlines, no control characters,
// at most one blank line in a row.
func normalizeBody(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
	s = strings.TrimSpace(s)
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
