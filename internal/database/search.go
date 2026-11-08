package database

import (
	"context"
	"database/sql"

	"github.com/FlavRomano/blog/internal/database/sqlc"
)

// SearchPosts runs an FTS5 query over published posts. It is hand-written
// because sqlc cannot parse FTS5 MATCH on a virtual table.
func SearchPosts(ctx context.Context, db *sql.DB, query string, limit int64) ([]sqlc.Post, error) {
	rows, err := db.QueryContext(ctx, `
SELECT p.id, p.slug, p.title, p.summary, p.body_md, p.status, p.published_at, p.created_at, p.updated_at
FROM posts_fts
JOIN posts p ON p.id = posts_fts.rowid
WHERE posts_fts MATCH ? AND p.status = 'published'
ORDER BY rank
LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []sqlc.Post
	for rows.Next() {
		var p sqlc.Post
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.BodyMd, &p.Status, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
