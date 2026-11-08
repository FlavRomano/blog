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
