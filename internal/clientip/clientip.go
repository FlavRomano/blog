// Package clientip derives a privacy-preserving client identity (ADR 0005).
package clientip

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
)

type Resolver struct {
	salt        []byte
	trustTunnel bool
}

func New(salt string, trustTunnel bool) *Resolver {
	return &Resolver{salt: []byte(salt), trustTunnel: trustTunnel}
}

// IP returns the client address. CF-Connecting-IP is honoured only when the
// request arrives from loopback, i.e. via the local cloudflared tunnel.
func (r *Resolver) IP(req *http.Request) netip.Addr {
	remote := remoteAddr(req)
	if r.trustTunnel && remote.IsLoopback() {
		if h := req.Header.Get("CF-Connecting-IP"); h != "" {
			if a, err := netip.ParseAddr(h); err == nil {
				return a.Unmap()
			}
		}
	}
	return remote
}

func remoteAddr(req *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

// Hash returns a salted HMAC of the client IP. IPv6 addresses are reduced to
// their /64 so rotating interface identifiers do not evade limits.
func (r *Resolver) Hash(req *http.Request) string {
	ip := r.IP(req)
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			ip = p.Addr()
		}
	}
	m := hmac.New(sha256.New, r.salt)
	m.Write([]byte(ip.String()))
	return hex.EncodeToString(m.Sum(nil))[:32]
}
