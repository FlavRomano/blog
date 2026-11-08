-- name: CreatePost :one
INSERT INTO posts (slug, title, summary, body_md, status, published_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPostBySlug :one
SELECT * FROM posts WHERE slug = ?;

-- name: ListPublishedPosts :many
SELECT * FROM posts
WHERE status = 'published'
ORDER BY published_at DESC
LIMIT ? OFFSET ?;

-- name: CountPublishedPosts :one
SELECT count(*) FROM posts WHERE status = 'published';

-- name: PublishDuePosts :execrows
UPDATE posts
SET status = 'published', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE status = 'scheduled' AND published_at <= ?;
