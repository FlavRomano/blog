package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/database"
)

const goodPassword = "correct horse battery"

var testKey = strings.Repeat("ab", 32)

func TestPasswordHashing(t *testing.T) {
	h, err := auth.HashPassword("s3cret-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") || !auth.VerifyPassword(h, "s3cret-passphrase") {
		t.Fatal("hash should verify")
	}
	if auth.VerifyPassword(h, "wrong") || auth.VerifyPassword("garbage", "x") || auth.VerifyPassword("", "") {
		t.Fatal("wrong password or malformed hash must not verify")
	}
}

// RFC 6238 appendix B, SHA1 secret, T=59s: 8-digit 94287082 => 6-digit 287082.
func TestTOTPRFCVector(t *testing.T) {
	secret := []byte("12345678901234567890")
	if got := auth.CodeAt(secret, time.Unix(59, 0)); got != "287082" {
		t.Fatalf("got %s", got)
	}
	if got := auth.CodeAt(secret, time.Unix(1111111109, 0)); got != "081804" {
		t.Fatalf("got %s", got)
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	code := auth.CodeAt(secret, now)

	step, ok := auth.VerifyTOTP(secret, code, now, 0)
	if !ok {
		t.Fatal("current code rejected")
	}
	if _, ok := auth.VerifyTOTP(secret, code, now, step); ok {
		t.Error("replay accepted")
	}
	if _, ok := auth.VerifyTOTP(secret, code, now.Add(30*time.Second), 0); !ok {
		t.Error("one step of drift should be accepted")
	}
	if _, ok := auth.VerifyTOTP(secret, code, now.Add(120*time.Second), 0); ok {
		t.Error("stale code accepted")
	}
	if _, ok := auth.VerifyTOTP(secret, "12345", now, 0); ok {
		t.Error("short code accepted")
	}
}

func TestKeysSealOpen(t *testing.T) {
	k, err := auth.NewKeys(testKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := k.Seal([]byte("secret"))
	if got, err := k.Open(sealed); err != nil || string(got) != "secret" {
		t.Fatalf("open: %q %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := k.Open(sealed); err == nil {
		t.Error("tampered ciphertext accepted")
	}
	other, _ := auth.NewKeys(strings.Repeat("cd", 32))
	fresh, _ := k.Seal([]byte("x"))
	if _, err := other.Open(fresh); err == nil {
		t.Error("wrong key accepted")
	}
	if _, err := auth.NewKeys("short"); err == nil {
		t.Error("bad key accepted")
	}
}

type env struct {
	svc *auth.Service
	enr auth.Enrollment
	now *time.Time
}

func (e env) code() string {
	secret, _ := auth.DecodeSecret(e.enr.Secret)
	return auth.CodeAt(secret, *e.now)
}

func newEnv(t *testing.T) env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keys, _ := auth.NewKeys(testKey)
	now := time.Unix(1_700_000_000, 0).UTC()
	e := env{now: &now}
	e.svc = auth.NewService(db, keys, auth.Options{Issuer: "Test", Now: func() time.Time { return now }})
	e.enr, err = e.svc.Enroll(context.Background(), goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEnrollRejectsWeakPassword(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Enroll(context.Background(), "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Fatalf("got %v", err)
	}
	if len(e.enr.RecoveryCodes) != 10 || !strings.HasPrefix(e.enr.URI, "otpauth://totp/") {
		t.Fatalf("enrollment: %+v", e.enr)
	}
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	if _, err := e.svc.Login(ctx, "ip1", "wrong password!!", e.code()); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := e.svc.Login(ctx, "ip1", goodPassword, "000000"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong code: %v", err)
	}
	sess, err := e.svc.Login(ctx, "ip1", goodPassword, e.code())
	if err != nil {
		t.Fatal(err)
	}
	// Same code again within the same step is a replay.
	if _, err := e.svc.Login(ctx, "ip1", goodPassword, e.code()); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("replay: %v", err)
	}

	got, err := e.svc.Authenticate(ctx, sess.Token)
	if err != nil || got.CSRF != sess.CSRF {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, "bogus"); !errors.Is(err, auth.ErrNoSession) {
		t.Fatal("bogus token accepted")
	}
	if err := e.svc.Logout(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatal("session survived logout")
	}
}

func TestSessionExpiry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess, _ := e.svc.Login(ctx, "ip", goodPassword, e.code())

	*e.now = e.now.Add(3 * time.Hour) // beyond the idle timeout
	if _, err := e.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatal("idle session accepted")
	}

	sess, _ = e.svc.Login(ctx, "ip", goodPassword, e.code())
	for range 8 { // stay active past the absolute lifetime
		*e.now = e.now.Add(100 * time.Minute)
		_, _ = e.svc.Authenticate(ctx, sess.Token)
	}
	if _, err := e.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatal("session outlived absolute lifetime")
	}
}

func TestRecoveryCodeIsSingleUse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rc := strings.ToLower(e.enr.RecoveryCodes[0])

	if _, err := e.svc.Login(ctx, "ip", goodPassword, rc); err != nil {
		t.Fatalf("recovery login: %v", err)
	}
	if _, err := e.svc.Login(ctx, "ip", goodPassword, rc); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("reuse: %v", err)
	}
	if _, err := e.svc.Login(ctx, "ip", "wrong password!!", e.enr.RecoveryCodes[1]); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("recovery code must not bypass password: %v", err)
	}
}

func TestLockout(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 5 {
		_, _ = e.svc.Login(ctx, "attacker", "nope nope nope", "000000")
	}
	if _, err := e.svc.Login(ctx, "attacker", goodPassword, e.code()); !errors.Is(err, auth.ErrLockedOut) {
		t.Fatalf("locked ip should be refused even with valid credentials: %v", err)
	}
	if _, err := e.svc.Login(ctx, "owner", goodPassword, e.code()); err != nil {
		t.Fatalf("other ip should still log in: %v", err)
	}
	*e.now = e.now.Add(16 * time.Minute)
	if _, err := e.svc.Login(ctx, "attacker", goodPassword, e.code()); err != nil {
		t.Fatalf("lockout should expire: %v", err)
	}
}

func TestReEnrollInvalidatesEverything(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess, _ := e.svc.Login(ctx, "ip", goodPassword, e.code())
	if _, err := e.svc.Enroll(ctx, "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrNoSession) {
		t.Error("old session survived reset")
	}
	if _, err := e.svc.Login(ctx, "ip", goodPassword, e.code()); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Error("old password still works")
	}
}

func TestLoginWithoutAdmin(t *testing.T) {
	db, _ := database.Open(filepath.Join(t.TempDir(), "t.db"))
	t.Cleanup(func() { _ = db.Close() })
	keys, _ := auth.NewKeys(testKey)
	svc := auth.NewService(db, keys, auth.Options{})
	if _, err := svc.Login(context.Background(), "ip", goodPassword, "123456"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("got %v", err)
	}
}
