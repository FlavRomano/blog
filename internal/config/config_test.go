package config

import "testing"

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("BLOG_ADDR", "")
	t.Setenv("BLOG_DB_PATH", "/tmp/x.db")
	t.Setenv("BLOG_TRUST_TUNNEL", "true")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:8080" || c.DBPath != "/tmp/x.db" || !c.TrustTunnel {
		t.Fatalf("unexpected config: %+v", c)
	}
}
