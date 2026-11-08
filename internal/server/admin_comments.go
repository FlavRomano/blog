package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/database/sqlc"
)

var hashRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

type adminComment struct {
	ID                 int64
	Author, Body       string
	Hash, PostTitle    string
	PostSlug, Dated    string
	New, Banned, HasIP bool
	ShortHash          string
}

type banRow struct {
	Hash, ShortHash, Dated string
	Comments               int64
}

type commentsAdminData struct {
	Comments []adminComment
	Bans     []banRow
	Flash    string
}

func (s *Server) commentsAdmin(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListCommentsAdmin(r.Context())
	if err != nil {
		s.serverError(w, "list comments", err)
		return
	}
	bans, err := s.q.ListBans(r.Context())
	if err != nil {
		s.serverError(w, "list bans", err)
		return
	}
	data := commentsAdminData{Flash: r.URL.Query().Get("flash")}
	for _, c := range rows {
		data.Comments = append(data.Comments, adminComment{
			ID: c.ID, Author: c.Author, Body: c.Body, Hash: c.IpHash, ShortHash: short(c.IpHash),
			PostTitle: c.PostTitle, PostSlug: c.PostSlug, Dated: s.localCreated(c.CreatedAt),
			New: c.Seen == 0, Banned: c.Banned, HasIP: c.IpHash != "",
		})
	}
	for _, b := range bans {
		data.Bans = append(data.Bans, banRow{Hash: b.IpHash, ShortHash: short(b.IpHash), Dated: s.localCreated(b.CreatedAt), Comments: b.CommentCount})
	}
	// Render first so the "new" markers show, then mark everything seen.
	page := s.adminPage(r, "Comments", data)
	if err := s.q.MarkCommentsSeen(r.Context()); err != nil {
		s.log.Error("mark seen", "err", err)
	}
	s.render(w, http.StatusOK, "admin_comments.html", page)
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func (s *Server) localCreated(v string) string {
	t, err := time.Parse(auth.TimeFormat, v)
	if err != nil {
		return v
	}
	return t.In(s.loc).Format("2006-01-02 15:04")
}

func (s *Server) commentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.removeComment(r.Context(), id); err != nil {
		s.serverError(w, "delete comment", err)
		return
	}
	http.Redirect(w, r, "/admin/comments?flash=deleted", http.StatusSeeOther)
}

// removeComment deletes a comment, or tombstones it when replies exist.
// Tombstoned ancestors left without replies are removed as well.
func (s *Server) removeComment(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)

	c, err := q.GetComment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	kids, err := q.CountCommentChildren(ctx, sql.NullInt64{Int64: id, Valid: true})
	if err != nil {
		return err
	}
	if kids > 0 {
		if err := q.TombstoneComment(ctx, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := q.DeleteComment(ctx, id); err != nil {
		return err
	}
	for parent := c.ParentID; parent.Valid; {
		p, err := q.GetComment(ctx, parent.Int64)
		if err != nil {
			return err
		}
		n, err := q.CountCommentChildren(ctx, parent)
		if err != nil {
			return err
		}
		if p.Deleted == 0 || n > 0 {
			break
		}
		if err := q.DeleteComment(ctx, p.ID); err != nil {
			return err
		}
		parent = p.ParentID
	}
	return tx.Commit()
}

func (s *Server) commentBan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.q.GetComment(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get comment", err)
		return
	}
	if c.IpHash == "" {
		http.Redirect(w, r, "/admin/comments?flash=nohash", http.StatusSeeOther)
		return
	}
	if err := s.q.InsertBan(r.Context(), sqlc.InsertBanParams{IpHash: c.IpHash, CreatedAt: time.Now().UTC().Format(auth.TimeFormat)}); err != nil {
		s.serverError(w, "ban", err)
		return
	}
	http.Redirect(w, r, "/admin/comments?flash=banned", http.StatusSeeOther)
}

func (s *Server) banDelete(w http.ResponseWriter, r *http.Request) {
	h := r.PathValue("hash")
	if !hashRe.MatchString(h) {
		http.NotFound(w, r)
		return
	}
	if err := s.q.DeleteBan(r.Context(), h); err != nil {
		s.serverError(w, "unban", err)
		return
	}
	http.Redirect(w, r, "/admin/comments?flash=unbanned", http.StatusSeeOther)
}
