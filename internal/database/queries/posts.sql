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

-- name: ListPublishedPostsByTag :many
SELECT p.* FROM posts p
JOIN post_tags pt ON pt.post_id = p.id
JOIN tags t ON t.id = pt.tag_id
WHERE p.status = 'published' AND t.slug = ?
ORDER BY p.published_at DESC
LIMIT ? OFFSET ?;

-- name: CountPublishedPostsByTag :one
SELECT count(*) FROM posts p
JOIN post_tags pt ON pt.post_id = p.id
JOIN tags t ON t.id = pt.tag_id
WHERE p.status = 'published' AND t.slug = ?;

-- name: ListPublishedPostRefs :many
SELECT slug, updated_at FROM posts
WHERE status = 'published'
ORDER BY published_at DESC;
