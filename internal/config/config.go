// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Addr        string
	DBPath      string
	UploadsDir  string
	LogLevel    string
	TOTPKeyHex  string // 32-byte key, hex; encrypts the TOTP secret (ADR 0004)
	IPHashSalt  string // stable salt for IP hashing (ADR 0005)
	TrustTunnel bool   // trust CF-Connecting-IP only when behind the tunnel
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
	}
	if c.DBPath == "" {
		return c, fmt.Errorf("BLOG_DB_PATH must not be empty")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
