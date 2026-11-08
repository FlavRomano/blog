package notify

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhook(t *testing.T) {
	got := make(chan [3]string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- [3]string{r.Header.Get("Title"), string(b), r.Header.Get("Click")}
	}))
	defer ts.Close()
	Webhook(ts.URL, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))("T", "hello", "https://x/y")
	select {
	case g := <-got:
		if g != [3]string{"T", "hello", "https://x/y"} {
			t.Fatalf("got %v", g)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no request")
	}
}
