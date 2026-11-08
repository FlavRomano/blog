package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default, required by authenticator apps
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func GenerateSecret() ([]byte, error) {
	s := make([]byte, 20)
	_, err := rand.Read(s)
	return s, err
}

func EncodeSecret(s []byte) string { return b32.EncodeToString(s) }

func DecodeSecret(s string) ([]byte, error) {
	return b32.DecodeString(strings.ToUpper(strings.ReplaceAll(s, " ", "")))
}

// OTPAuthURI is the provisioning URI authenticator apps import (usually via QR).
func OTPAuthURI(issuer, account string, secret []byte) string {
	q := url.Values{}
	q.Set("secret", EncodeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer) + ":" + url.PathEscape(account) + "?" + q.Encode()
}

func step(t time.Time) int64 { return t.Unix() / totpPeriod }

func hotp(secret []byte, counter int64, digits int) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter)) //nolint:gosec // counter is non-negative
	m := hmac.New(sha1.New, secret)
	m.Write(buf[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, v%mod)
}

// CodeAt returns the code an authenticator app shows at time t.
func CodeAt(secret []byte, t time.Time) string { return hotp(secret, step(t), totpDigits) }

// VerifyTOTP accepts the current step and one step either side for clock
// drift, but never a step at or before lastStep (replay protection). It
// returns the matched step.
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	var matched int64
	ok := false
	for d := int64(-1); d <= 1; d++ {
		s := step(now) + d
		if s <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(hotp(secret, s, totpDigits)), []byte(code)) == 1 {
			matched, ok = s, true
		}
	}
	return matched, ok
}

const recoveryCodeCount = 10

// GenerateRecoveryCodes returns codes like "ABCD-EFGH-IJKL-MNOP" (80 bits each).
func GenerateRecoveryCodes() ([]string, error) {
	out := make([]string, recoveryCodeCount)
	for i := range out {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s := b32.EncodeToString(b)
		out[i] = s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
	}
	return out, nil
}

func normalizeRecoveryCode(c string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}
