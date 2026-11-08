package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/slug"
)

const (
	maxTitleLen   = 200
	maxSlugLen    = 100
	maxSummaryLen = 300
	maxBodyBytes  = 200_000
	maxTags       = 10
	maxTagLen     = 30
	timeInput     = "2006-01-02T15:04"
)

type postForm struct {
	ID                                       int64
	Title, Slug, Summary, Body, Tags, Status string
	PublishAt                                string
	Errors                                   map[string]string
	Saved                                    bool
	Preview                                  template.HTML
}

type tagInput struct{ Slug, Name string }

type postInput struct {
	Slug, Title, Summary, Body, Status string
	PublishedAt                        sql.NullString
	Tags                               []tagInput
}

func (s *Server) postNew(w http.ResponseWriter, r *http.Request) {
	s.renderForm(w, r, http.StatusOK, postForm{Status: "draft"})
}

func (s *Server) renderForm(w http.ResponseWriter, r *http.Request, status int, f postForm) {
	if f.Preview == "" && f.Body != "" {
		if res, err := s.md.Render(f.Body); err == nil {
			f.Preview = res.HTML
		}
	}
	title := "New post"
	if f.ID != 0 {
		title = "Edit post"
	}
	s.render(w, status, "admin_post.html", s.adminPage(r, title, f))
}

func postID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) postEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := postID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	p, err := s.q.GetPostByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get post", err)
		return
	}
	tags, err := s.q.ListPostTags(r.Context(), p.ID)
	if err != nil {
		s.serverError(w, "list tags", err)
		return
	}
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	f := postForm{
		ID: p.ID, Title: p.Title, Slug: p.Slug, Summary: p.Summary, Body: p.BodyMd,
		Tags: strings.Join(names, ", "), Status: p.Status, Saved: r.URL.Query().Get("saved") == "1",
	}
	if p.PublishedAt.Valid {
		f.PublishAt = parseTime(p.PublishedAt.String).In(s.loc).Format(timeInput)
	}
	s.renderForm(w, r, http.StatusOK, f)
}

func (s *Server) postCreate(w http.ResponseWriter, r *http.Request) { s.savePostRequest(w, r, 0) }

func (s *Server) postUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := postID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	s.savePostRequest(w, r, id)
}

func (s *Server) savePostRequest(w http.ResponseWriter, r *http.Request, id int64) {
	var existing *sqlc.Post
	if id != 0 {
		p, err := s.q.GetPostByID(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, "get post", err)
			return
		}
		existing = &p
	}

	f, in := s.parsePostForm(r, existing, time.Now())
	f.ID = id
	if len(f.Errors) > 0 {
		s.renderForm(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	newID, err := s.savePost(r.Context(), id, in)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: posts.slug") {
			f.Errors["slug"] = "This slug is already used by another post."
			s.renderForm(w, r, http.StatusUnprocessableEntity, f)
			return
		}
		s.serverError(w, "save post", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d?saved=1", newID), http.StatusSeeOther)
}

func (s *Server) parsePostForm(r *http.Request, existing *sqlc.Post, now time.Time) (postForm, postInput) {
	val := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	f := postForm{
		Title: val("title"), Slug: val("slug"), Summary: val("summary"), Tags: val("tags"),
		Status: val("status"), PublishAt: val("publish_at"),
		Body:   strings.ReplaceAll(r.PostFormValue("body"), "\r\n", "\n"),
		Errors: map[string]string{},
	}
	in := postInput{Title: f.Title, Summary: f.Summary, Body: f.Body, Status: f.Status}

	switch {
	case f.Title == "":
		f.Errors["title"] = "Title is required."
	case utf8.RuneCountInString(f.Title) > maxTitleLen:
		f.Errors["title"] = fmt.Sprintf("Title must be at most %d characters.", maxTitleLen)
	}

	src := f.Slug
	if src == "" {
		src = f.Title
	}
	f.Slug = slug.Make(src)
	in.Slug = f.Slug
	switch {
	case f.Slug == "":
		f.Errors["slug"] = "Slug is required (use letters or digits)."
	case len(f.Slug) > maxSlugLen:
		f.Errors["slug"] = fmt.Sprintf("Slug must be at most %d characters.", maxSlugLen)
	}

	if utf8.RuneCountInString(f.Summary) > maxSummaryLen {
		f.Errors["summary"] = fmt.Sprintf("Summary must be at most %d characters.", maxSummaryLen)
	}
	switch {
	case strings.TrimSpace(f.Body) == "":
		f.Errors["body"] = "Body is required."
	case len(f.Body) > maxBodyBytes:
		f.Errors["body"] = "Body is too long."
	}

	seen := map[string]bool{}
	for _, raw := range strings.Split(f.Tags, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		sl := slug.Make(name)
		if sl == "" || utf8.RuneCountInString(name) > maxTagLen {
			f.Errors["tags"] = fmt.Sprintf("Tags must contain letters or digits and be at most %d characters.", maxTagLen)
			break
		}
		if !seen[sl] {
			seen[sl] = true
			in.Tags = append(in.Tags, tagInput{Slug: sl, Name: name})
		}
	}
	if len(in.Tags) > maxTags {
		f.Errors["tags"] = fmt.Sprintf("At most %d tags.", maxTags)
	}

	var when time.Time
	if f.PublishAt != "" {
		t, err := time.ParseInLocation(timeInput, f.PublishAt, s.loc)
		if err != nil {
			f.Errors["publish_at"] = "Invalid date and time."
		}
		when = t
	}
	switch f.Status {
	case "draft":
	case "scheduled":
		switch {
		case f.Errors["publish_at"] != "":
		case when.IsZero():
			f.Errors["publish_at"] = "Pick a publish time for a scheduled post."
		case !when.After(now):
			f.Errors["publish_at"] = "A scheduled time must be in the future."
		default:
			in.PublishedAt = sql.NullString{String: when.UTC().Format(auth.TimeFormat), Valid: true}
		}
	case "published":
		switch {
		case f.Errors["publish_at"] != "":
		case !when.IsZero() && when.After(now):
			f.Errors["publish_at"] = "Use the scheduled status for a future time."
		case !when.IsZero():
			in.PublishedAt = sql.NullString{String: when.UTC().Format(auth.TimeFormat), Valid: true}
		case existing != nil && existing.PublishedAt.Valid && existing.Status == "published":
			in.PublishedAt = existing.PublishedAt
		default:
			in.PublishedAt = sql.NullString{String: now.UTC().Format(auth.TimeFormat), Valid: true}
		}
	default:
		f.Errors["status"] = "Choose a valid status."
	}
	return f, in
}

func (s *Server) savePost(ctx context.Context, id int64, in postInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)

	if id == 0 {
		p, err := q.CreatePost(ctx, sqlc.CreatePostParams{
			Slug: in.Slug, Title: in.Title, Summary: in.Summary, BodyMd: in.Body, Status: in.Status, PublishedAt: in.PublishedAt,
		})
		if err != nil {
			return 0, err
		}
		id = p.ID
	} else {
		if err := q.UpdatePost(ctx, sqlc.UpdatePostParams{
			Slug: in.Slug, Title: in.Title, Summary: in.Summary, BodyMd: in.Body, Status: in.Status, PublishedAt: in.PublishedAt, ID: id,
		}); err != nil {
			return 0, err
		}
		if err := q.ClearPostTags(ctx, id); err != nil {
			return 0, err
		}
	}
	for _, t := range in.Tags {
		tag, err := q.UpsertTag(ctx, sqlc.UpsertTagParams{Slug: t.Slug, Name: t.Name})
		if err != nil {
			return 0, err
		}
		if err := q.AddPostTag(ctx, sqlc.AddPostTagParams{PostID: id, TagID: tag.ID}); err != nil {
			return 0, err
		}
	}
	if err := q.DeleteOrphanTags(ctx); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Server) postDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := postID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.serverError(w, "begin", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	if err := q.DeletePost(r.Context(), id); err != nil {
		s.serverError(w, "delete post", err)
		return
	}
	if err := q.DeleteOrphanTags(r.Context()); err != nil {
		s.serverError(w, "delete orphan tags", err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, "commit", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// preview renders Markdown for the htmx live preview, using the same
// sanitising pipeline as public pages.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	body := r.PostFormValue("body")
	if len(body) > maxBodyBytes {
		http.Error(w, "body too long", http.StatusRequestEntityTooLarge)
		return
	}
	res, err := s.md.Render(body)
	if err != nil {
		s.serverError(w, "preview", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(res.HTML))
}
