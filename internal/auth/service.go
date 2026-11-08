package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FlavRomano/blog/internal/database/sqlc"
)

// TimeFormat is the fixed UTC layout stored in the database so that string
// comparison orders chronologically.
const TimeFormat = "2006-01-02T15:04:05Z"

const (
	maxFailuresPerIP  = 5
	maxFailuresGlobal = 30
	failureWindow     = 15 * time.Minute
	sessionTTL        = 12 * time.Hour
	sessionIdle       = 2 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLockedOut          = errors.New("too many failed attempts")
	ErrNoSession          = errors.New("no valid session")
	ErrWeakPassword       = fmt.Errorf("password must be at least %d characters", MinPasswordLen)
)

type Service struct {
	db     *sql.DB
	q      *sqlc.Queries
	keys   *Keys
	issuer string
	now    func() time.Time

	dummyOnce sync.Once
	dummyHash string
}

type Options struct {
	Issuer string
	Now    func() time.Time // defaults to time.Now
}

func NewService(db *sql.DB, keys *Keys, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), keys: keys, issuer: o.Issuer, now: o.Now}
}

type Enrollment struct {
	Secret        string // base32, for manual entry
	URI           string // otpauth:// provisioning URI
	RecoveryCodes []string
}

// HasAdmin reports whether credentials have been enrolled.
func (s *Service) HasAdmin(ctx context.Context) (bool, error) {
	_, err := s.q.GetAdmin(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Enroll creates or replaces the admin credentials, issues a fresh TOTP secret and
// recovery codes, and invalidates every session and lockout.
func (s *Service) Enroll(ctx context.Context, password string) (Enrollment, error) {
	if len([]rune(password)) < MinPasswordLen {
		return Enrollment{}, ErrWeakPassword
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Enrollment{}, err
	}
	secret, err := GenerateSecret()
	if err != nil {
		return Enrollment{}, err
	}
	sealed, err := s.keys.Seal(secret)
	if err != nil {
		return Enrollment{}, err
	}
	codes, err := GenerateRecoveryCodes()
	if err != nil {
		return Enrollment{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	if err := q.UpsertAdmin(ctx, sqlc.UpsertAdminParams{PasswordHash: hash, TotpSecretEnc: sealed}); err != nil {
		return Enrollment{}, err
	}
	if err := q.DeleteRecoveryCodes(ctx); err != nil {
		return Enrollment{}, err
	}
	for _, c := range codes {
		if err := q.InsertRecoveryCode(ctx, s.keys.MAC(normalizeRecoveryCode(c))); err != nil {
			return Enrollment{}, err
		}
	}
	if err := q.DeleteAllSessions(ctx); err != nil {
		return Enrollment{}, err
	}
	if err := q.ClearLoginFailures(ctx); err != nil {
		return Enrollment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: EncodeSecret(secret), URI: OTPAuthURI(s.issuer, "admin", secret), RecoveryCodes: codes}, nil
}

type Session struct {
	Token     string // raw cookie value; only its hash is stored
	CSRF      string
	ExpiresAt time.Time
}

// Login verifies the password and a TOTP or recovery code, honouring the
// per-IP and global failure limits. ipHash is the salted hash from package clientip.
func (s *Service) Login(ctx context.Context, ipHash, password, code string) (Session, error) {
	now := s.now()
	since := now.Add(-failureWindow).UTC().Format(TimeFormat)

	perIP, err := s.q.CountLoginFailuresByIP(ctx, sqlc.CountLoginFailuresByIPParams{IpHash: ipHash, At: since})
	if err != nil {
		return Session{}, err
	}
	global, err := s.q.CountLoginFailures(ctx, since)
	if err != nil {
		return Session{}, err
	}
	if perIP >= maxFailuresPerIP || global >= maxFailuresGlobal {
		return Session{}, ErrLockedOut
	}

	ok, err := s.checkCredentials(ctx, password, code, now)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		if err := s.q.RecordLoginFailure(ctx, sqlc.RecordLoginFailureParams{IpHash: ipHash, At: now.UTC().Format(TimeFormat)}); err != nil {
			return Session{}, err
		}
		return Session{}, ErrInvalidCredentials
	}
	if err := s.q.ClearLoginFailuresByIP(ctx, ipHash); err != nil {
		return Session{}, err
	}
	return s.newSession(ctx, now)
}

func (s *Service) checkCredentials(ctx context.Context, password, code string, now time.Time) (bool, error) {
	adm, err := s.q.GetAdmin(ctx)
	hash := adm.PasswordHash
	if errors.Is(err, sql.ErrNoRows) {
		hash = s.dummy()
	} else if err != nil {
		return false, err
	}
	pwOK := VerifyPassword(hash, password)
	if !pwOK || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	code = strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code))
	if len(code) == totpDigits && isDigits(code) {
		secret, err := s.keys.Open(adm.TotpSecretEnc)
		if err != nil {
			return false, fmt.Errorf("decrypt totp secret (wrong BLOG_TOTP_KEY?): %w", err)
		}
		step, ok := VerifyTOTP(secret, code, now, adm.TotpLastStep)
		if !ok {
			return false, nil
		}
		n, err := s.q.AdvanceTOTPStep(ctx, step)
		return n == 1, err
	}
	n, err := s.q.UseRecoveryCode(ctx, sqlc.UseRecoveryCodeParams{
		UsedAt:   sql.NullString{String: now.UTC().Format(TimeFormat), Valid: true},
		CodeHash: s.keys.MAC(normalizeRecoveryCode(code)),
	})
	return n == 1, err
}

// dummy is verified against when no admin exists so timing does not reveal that.
func (s *Service) dummy() string {
	s.dummyOnce.Do(func() { s.dummyHash, _ = HashPassword("dummy-password-for-timing") })
	return s.dummyHash
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (s *Service) newSession(ctx context.Context, now time.Time) (Session, error) {
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	exp := now.Add(sessionTTL)
	ts := now.UTC().Format(TimeFormat)
	err = s.q.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hashToken(token), CsrfToken: csrf, CreatedAt: ts, LastSeenAt: ts, ExpiresAt: exp.UTC().Format(TimeFormat),
	})
	return Session{Token: token, CSRF: csrf, ExpiresAt: exp}, err
}

// Authenticate validates a session cookie value and refreshes its idle timer.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNoSession
	}
	row, err := s.q.GetSessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	exp, _ := time.Parse(TimeFormat, row.ExpiresAt)
	seen, _ := time.Parse(TimeFormat, row.LastSeenAt)
	if !now.Before(exp) || now.Sub(seen) >= sessionIdle {
		_ = s.q.DeleteSessionByTokenHash(ctx, row.TokenHash)
		return Session{}, ErrNoSession
	}
	if now.Sub(seen) > time.Minute {
		if err := s.q.TouchSession(ctx, sqlc.TouchSessionParams{LastSeenAt: now.UTC().Format(TimeFormat), ID: row.ID}); err != nil {
			return Session{}, err
		}
	}
	return Session{Token: token, CSRF: row.CsrfToken, ExpiresAt: exp}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.q.DeleteSessionByTokenHash(ctx, hashToken(token))
}

// Prune removes expired sessions and old login failures.
func (s *Service) Prune(ctx context.Context) error {
	now := s.now().UTC()
	if err := s.q.DeleteStaleSessions(ctx, sqlc.DeleteStaleSessionsParams{
		ExpiresAt: now.Format(TimeFormat), LastSeenAt: now.Add(-sessionIdle).Format(TimeFormat),
	}); err != nil {
		return err
	}
	return s.q.PruneLoginFailures(ctx, now.Add(-failureWindow).Format(TimeFormat))
}
