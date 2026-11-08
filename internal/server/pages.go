package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/markdown"
)

const pageSize = 10

type pager struct {
	Page, Total int
	Prev, Next  string
}

func newPager(base string, page, count int) pager {
	total := (count + pageSize - 1) / pageSize
	if total < 1 {
		total = 1
	}
	link := func(n int) string {
		if n == 1 {
			return base
		}
		return fmt.Sprintf("%s?page=%d", base, n)
	}
	p := pager{Page: page, Total: total}
	if page > 1 {
		p.Prev = link(page - 1)
	}
	if page < total {
		p.Next = link(page + 1)
	}
	return p
}

func pageNumber(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

type listData struct {
	Posts []postView
	Pager pager
	Tag   sqlc.Tag
}

func (s *Server) views(r *http.Request, posts []sqlc.Post) ([]postView, error) {
	out := make([]postView, 0, len(posts))
	for _, p := range posts {
		v, err := s.toView(r, p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	page := pageNumber(r)
	count, err := s.q.CountPublishedPosts(r.Context())
	if err != nil {
		s.serverError(w, "count posts", err)
		return
	}
	posts, err := s.q.ListPublishedPosts(r.Context(), sqlc.ListPublishedPostsParams{Limit: pageSize, Offset: int64(page-1) * pageSize})
	if err != nil {
		s.serverError(w, "list posts", err)
		return
	}
	vs, err := s.views(r, posts)
	if err != nil {
		s.serverError(w, "post views", err)
		return
	}
	title := ""
	if page > 1 {
		title = fmt.Sprintf("Posts, page %d", page)
	}
	s.render(w, http.StatusOK, "index.html", pageData{
		Title: title, Path: "/", Data: listData{Posts: vs, Pager: newPager("/", page, int(count))},
	})
}

func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	p, err := s.q.GetPostBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && p.Status != "published") {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get post", err)
		return
	}
	s.showPost(w, r, p, http.StatusOK, commentForm{})
}

// showPost renders a published post with its comment thread and form.
func (s *Server) showPost(w http.ResponseWriter, r *http.Request, p sqlc.Post, status int, form commentForm) {
	v, err := s.toView(r, p)
	if err != nil {
		s.serverError(w, "post view", err)
		return
	}
	res, err := s.renderBody(p)
	if err != nil {
		s.serverError(w, "render markdown", err)
		return
	}
	var toc []markdown.TOCEntry
	if len(res.TOC) >= 2 {
		toc = res.TOC
	}
	page := postPage{Post: v, HTML: res.HTML, TOC: toc, CommentsEnabled: s.commentsEnabled(), Form: form, TurnstileKey: s.cfg.TurnstileSiteKey}
	if page.CommentsEnabled {
		rows, err := s.q.ListCommentsByPost(r.Context(), p.ID)
		if err != nil {
			s.serverError(w, "list comments", err)
			return
		}
		page.Comments, page.CommentCount = s.buildComments(rows)
		if page.Form.ParentID == 0 {
			page.Form.ParentID, _ = strconv.ParseInt(r.URL.Query().Get("reply"), 10, 64)
		}
		if page.Form.ParentID != 0 {
			page.Form.ReplyTo = ""
			for _, c := range rows {
				if c.ID == page.Form.ParentID && c.Deleted == 0 {
					page.Form.ReplyTo = c.Author
				}
			}
			if page.Form.ReplyTo == "" {
				page.Form.ParentID = 0
			}
		}
	}
	s.render(w, status, "post.html", pageData{
		Title: p.Title, Description: p.Summary, Path: "/posts/" + p.Slug, Nav: "/",
		OGType: "article", PublishedTime: v.DateISO, Tags: v.Tags, Data: page,
	})
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	ts, err := s.q.ListTagsWithCounts(r.Context())
	if err != nil {
		s.serverError(w, "list tags", err)
		return
	}
	s.render(w, http.StatusOK, "tags.html", pageData{Title: "Tags", Path: "/tags", Description: "Browse posts by tag.", Data: ts})
}

func (s *Server) tag(w http.ResponseWriter, r *http.Request) {
	t, err := s.q.GetTagBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get tag", err)
		return
	}
	page := pageNumber(r)
	count, err := s.q.CountPublishedPostsByTag(r.Context(), t.Slug)
	if err != nil {
		s.serverError(w, "count tag posts", err)
		return
	}
	if count == 0 {
		s.notFound(w, r)
		return
	}
	posts, err := s.q.ListPublishedPostsByTag(r.Context(), sqlc.ListPublishedPostsByTagParams{Slug: t.Slug, Limit: pageSize, Offset: int64(page-1) * pageSize})
	if err != nil {
		s.serverError(w, "list tag posts", err)
		return
	}
	vs, err := s.views(r, posts)
	if err != nil {
		s.serverError(w, "post views", err)
		return
	}
	base := "/tags/" + t.Slug
	s.render(w, http.StatusOK, "tag.html", pageData{
		Title: "#" + t.Name, Path: base, Nav: "/tags", Description: "Posts tagged " + t.Name + ".",
		Data: listData{Posts: vs, Pager: newPager(base, page, int(count)), Tag: t},
	})
}

func (s *Server) static(tmpl, title, path, desc string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		s.render(w, http.StatusOK, tmpl, pageData{Title: title, Path: path, Description: desc})
	}
}
