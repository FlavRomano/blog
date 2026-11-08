-- name: UpsertTag :one
INSERT INTO tags (slug, name) VALUES (?, ?)
ON CONFLICT (slug) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: AddPostTag :exec
INSERT OR IGNORE INTO post_tags (post_id, tag_id) VALUES (?, ?);

-- name: ListPostTags :many
SELECT t.* FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
WHERE pt.post_id = ?
ORDER BY t.name;

-- name: GetTagBySlug :one
SELECT * FROM tags WHERE slug = ?;

-- name: ListTagsWithCounts :many
SELECT t.id, t.slug, t.name, count(p.id) AS post_count
FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
JOIN posts p ON p.id = pt.post_id AND p.status = 'published'
GROUP BY t.id
ORDER BY t.name;

-- name: ClearPostTags :exec
DELETE FROM post_tags WHERE post_id = ?;

-- name: DeleteOrphanTags :exec
DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM post_tags);
