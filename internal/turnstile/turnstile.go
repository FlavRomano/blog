// Package turnstile verifies Cloudflare Turnstile tokens.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type Verifier struct {
	Secret string
	URL    string       // defaults to DefaultURL
	Client *http.Client // defaults to a client with a 5s timeout
}

// Verify reports whether token is a valid, unused Turnstile response.
// An error means the verification service was unreachable or misbehaved.
func (v *Verifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if token == "" {
		return false, nil
	}
	endpoint := v.URL
	if endpoint == "" {
		endpoint = DefaultURL
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	form := url.Values{"secret": {v.Secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("turnstile: status %d", resp.StatusCode)
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return false, fmt.Errorf("turnstile: decode: %w", err)
	}
	return out.Success, nil
}
