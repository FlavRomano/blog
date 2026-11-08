// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr        string
	DBPath      string
	UploadsDir  string
	LogLevel    string
	TOTPKeyHex  string // 32-byte key, hex; encrypts the TOTP secret (ADR 0004)
	IPHashSalt  string // stable salt for IP hashing (ADR 0005)
	TrustTunnel bool   // trust CF-Connecting-IP only when behind the tunnel

	BaseURL         string // public origin, no trailing slash
	SiteName        string
	SiteDescription string
	AuthorName      string
	CFBeaconToken   string // Cloudflare Web Analytics token; empty disables the beacon
	Timezone        string // IANA zone used to read/show admin publish times

	TurnstileSiteKey string // Cloudflare Turnstile; both keys empty disables the check
	TurnstileSecret  string
	NotifyURL        string // ntfy-style webhook for new-comment notifications; empty disables
}

func Load() (Config, error) {
	c := Config{
		Addr:        env("BLOG_ADDR", "127.0.0.1:8080"),
		DBPath:      env("BLOG_DB_PATH", "data/blog.db"),
		UploadsDir:  env("BLOG_UPLOADS_DIR", "data/uploads"),
		LogLevel:    env("BLOG_LOG_LEVEL", "info"),
		TOTPKeyHex:  os.Getenv("BLOG_TOTP_KEY"),
		IPHashSalt:  os.Getenv("BLOG_IP_SALT"),
		TrustTunnel: os.Getenv("BLOG_TRUST_TUNNEL") == "true",

		BaseURL:         strings.TrimRight(env("BLOG_BASE_URL", "http://localhost:8080"), "/"),
		SiteName:        env("BLOG_SITE_NAME", "Flavio Romano"),
		SiteDescription: env("BLOG_SITE_DESCRIPTION", "Notes on software, systems and whatever I'm building."),
		AuthorName:      env("BLOG_AUTHOR_NAME", "Flavio Romano"),
		CFBeaconToken:   os.Getenv("BLOG_CF_BEACON_TOKEN"),
		Timezone:        env("BLOG_TIMEZONE", "UTC"),

		TurnstileSiteKey: os.Getenv("BLOG_TURNSTILE_SITE_KEY"),
		TurnstileSecret:  os.Getenv("BLOG_TURNSTILE_SECRET"),
		NotifyURL:        os.Getenv("BLOG_NOTIFY_URL"),
	}
	if c.DBPath == "" {
		return c, fmt.Errorf("BLOG_DB_PATH must not be empty")
	}
	if (c.TurnstileSiteKey == "") != (c.TurnstileSecret == "") {
		return c, fmt.Errorf("BLOG_TURNSTILE_SITE_KEY and BLOG_TURNSTILE_SECRET must be set together")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
