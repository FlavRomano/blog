-- name: ListCommentsByPost :many
SELECT id, parent_id, depth, author, body, deleted, created_at
FROM comments WHERE post_id = ? ORDER BY created_at, id;

-- name: GetComment :one
SELECT * FROM comments WHERE id = ?;

-- name: InsertComment :one
INSERT INTO comments (post_id, parent_id, depth, author, body, ip_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CountCommentsByIPSince :one
SELECT count(*) FROM comments WHERE ip_hash = ? AND created_at >= ?;

-- name: CountCommentsSince :one
SELECT count(*) FROM comments WHERE created_at >= ?;

-- name: CountCommentChildren :one
SELECT count(*) FROM comments WHERE parent_id = ?;

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = ?;

-- name: TombstoneComment :exec
UPDATE comments SET deleted = 1, author = '', body = '', ip_hash = '' WHERE id = ?;

-- name: ListCommentsAdmin :many
SELECT c.id, c.author, c.body, c.ip_hash, c.seen, c.created_at, p.title AS post_title, p.slug AS post_slug,
       EXISTS (SELECT 1 FROM bans b WHERE b.ip_hash = c.ip_hash) AS banned
FROM comments c JOIN posts p ON p.id = c.post_id
WHERE c.deleted = 0
ORDER BY c.created_at DESC, c.id DESC
LIMIT 200;

-- name: MarkCommentsSeen :exec
UPDATE comments SET seen = 1 WHERE seen = 0;

-- name: CountUnseenComments :one
SELECT count(*) FROM comments WHERE seen = 0 AND deleted = 0;

-- name: IsBanned :one
SELECT EXISTS (SELECT 1 FROM bans WHERE ip_hash = ?);

-- name: InsertBan :exec
INSERT INTO bans (ip_hash, created_at) VALUES (?, ?) ON CONFLICT (ip_hash) DO NOTHING;

-- name: DeleteBan :exec
DELETE FROM bans WHERE ip_hash = ?;

-- name: ListBans :many
SELECT b.ip_hash, b.created_at, (SELECT count(*) FROM comments c WHERE c.ip_hash = b.ip_hash) AS comment_count
FROM bans b ORDER BY b.created_at DESC;
