package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/database/sqlc"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateAndPostRoundTrip(t *testing.T) {
	db := open(t)
	q := sqlc.New(db)
	ctx := context.Background()

	_, err := q.CreatePost(ctx, sqlc.CreatePostParams{
		Slug: "hello", Title: "Hello", BodyMd: "gophers love sqlite",
		Status: "published", PublishedAt: sql.NullString{String: "2024-01-01T00:00:00Z", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := q.CountPublishedPosts(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	got, err := database.SearchPosts(ctx, db, "gophers", 10)
	if err != nil || len(got) != 1 || got[0].Slug != "hello" {
		t.Fatalf("search got=%v err=%v", got, err)
	}
}

func TestPublishDuePosts(t *testing.T) {
	db := open(t)
	q := sqlc.New(db)
	ctx := context.Background()
	for slug, at := range map[string]string{"past": "2020-01-01T00:00:00Z", "future": "2999-01-01T00:00:00Z"} {
		if _, err := q.CreatePost(ctx, sqlc.CreatePostParams{
			Slug: slug, Title: slug, BodyMd: "x", Status: "scheduled",
			PublishedAt: sql.NullString{String: at, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := q.PublishDuePosts(ctx, sql.NullString{String: "2024-01-01T00:00:00Z", Valid: true})
	if err != nil || n != 1 {
		t.Fatalf("published=%d err=%v", n, err)
	}
}
