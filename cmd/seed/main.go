// Command seed inserts sample posts for local development.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/slug"
)

const sample = "## Why Go\n\nSmall binaries, great tooling.\n\n### Example\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nSome `inline code`, a [link](https://go.dev) and a list:\n\n- one\n- two\n"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	q := sqlc.New(db)
	ctx := context.Background()

	for i := 1; i <= 12; i++ {
		title := fmt.Sprintf("Sample post %d", i)
		at := time.Now().UTC().AddDate(0, 0, -i*3).Format(time.RFC3339)
		p, err := q.CreatePost(ctx, sqlc.CreatePostParams{
			Slug: slug.Make(title), Title: title, Summary: "A sample post for local development.",
			BodyMd: sample, Status: "published", PublishedAt: sql.NullString{String: at, Valid: true},
		})
		if err != nil {
			log.Printf("skip %q: %v", title, err)
			continue
		}
		for _, name := range []string{"Go", map[bool]string{true: "SQLite", false: "Notes"}[i%2 == 0]} {
			t, err := q.UpsertTag(ctx, sqlc.UpsertTagParams{Slug: slug.Make(name), Name: name})
			if err != nil {
				log.Fatal(err)
			}
			if err := q.AddPostTag(ctx, sqlc.AddPostTagParams{PostID: p.ID, TagID: t.ID}); err != nil {
				log.Fatal(err)
			}
		}
	}
}
