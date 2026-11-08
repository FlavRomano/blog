package clientip

import (
	"net/http"
	"testing"
)

func req(remote, cf string) *http.Request {
	r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
	if cf != "" {
		r.Header.Set("CF-Connecting-IP", cf)
	}
	return r
}

func TestHeaderTrustedOnlyFromLoopbackWhenEnabled(t *testing.T) {
	tun := New("salt", true)
	if got := tun.IP(req("127.0.0.1:5555", "203.0.113.7")).String(); got != "203.0.113.7" {
		t.Errorf("tunnel request: %s", got)
	}
	if got := tun.IP(req("198.51.100.9:5555", "203.0.113.7")).String(); got != "198.51.100.9" {
		t.Errorf("spoofed header from outside accepted: %s", got)
	}
	if got := New("salt", false).IP(req("127.0.0.1:5555", "203.0.113.7")).String(); got != "127.0.0.1" {
		t.Errorf("trust disabled: %s", got)
	}
	if got := tun.IP(req("127.0.0.1:1", "not-an-ip")).String(); got != "127.0.0.1" {
		t.Errorf("garbage header: %s", got)
	}
}

func TestHash(t *testing.T) {
	a := New("salt-a", false)
	h := a.Hash(req("203.0.113.7:1", ""))
	if len(h) != 32 || h == "203.0.113.7" {
		t.Fatalf("bad hash %q", h)
	}
	if h != a.Hash(req("203.0.113.7:99", "")) {
		t.Error("hash should be stable across ports")
	}
	if h == New("salt-b", false).Hash(req("203.0.113.7:1", "")) {
		t.Error("salt must change the hash")
	}
	if a.Hash(req("[2001:db8::1]:1", "")) != a.Hash(req("[2001:db8::ffff]:1", "")) {
		t.Error("IPv6 should hash by /64")
	}
}
