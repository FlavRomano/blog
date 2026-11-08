// Package notify sends fire-and-forget notifications to an ntfy-style webhook.
package notify

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Func sends a notification. Implementations must not block the caller.
type Func func(title, message, link string)

// Webhook returns a Func that POSTs the message as plain text to url, using
// the ntfy convention (Title and Click headers). Failures are only logged.
func Webhook(url string, client *http.Client, log *slog.Logger) Func {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return func(title, message, link string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
			if err != nil {
				log.Error("notify", "err", err)
				return
			}
			req.Header.Set("Title", title)
			if link != "" {
				req.Header.Set("Click", link)
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Warn("notify failed", "err", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}
}
