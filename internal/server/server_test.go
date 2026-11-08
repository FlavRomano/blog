package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/server"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return server.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealthzAndHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/healthz", nil))
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("security headers missing: %v", rec.Header())
	}
}

func TestRobots(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/robots.txt", nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
}
