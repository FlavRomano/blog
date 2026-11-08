package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/skip2/go-qrcode"
	"golang.org/x/term"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database"
)

// runAdmin implements `blog admin setup|reset`, meant to be run on the Pi over SSH.
// Credentials are never created through the public web UI.
func runAdmin(args []string) error {
	if len(args) != 1 || (args[0] != "setup" && args[0] != "reset") {
		return usage()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	svc, err := newAuth(cfg, db)
	if err != nil {
		return err
	}
	if svc == nil {
		return errors.New("BLOG_TOTP_KEY is not set; generate one with: openssl rand -hex 32")
	}

	ctx := context.Background()
	has, err := svc.HasAdmin(ctx)
	if err != nil {
		return err
	}
	switch {
	case args[0] == "setup" && has:
		return errors.New("an admin already exists; use `blog admin reset` to replace its credentials")
	case args[0] == "reset" && has:
		fmt.Fprintln(os.Stderr, "This replaces the password, TOTP secret and recovery codes, and signs out every session.")
	}

	pw, err := readNewPassword()
	if err != nil {
		return err
	}
	enr, err := svc.Enroll(ctx, pw)
	if err != nil {
		return err
	}
	printEnrollment(os.Stdout, enr)
	return nil
}

func readNewPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // fd fits in int
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprintf(os.Stderr, "New password (min %d characters): ", auth.MinPasswordLen)
	a, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // fd fits in int
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // fd fits in int
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("passwords do not match")
	}
	return string(a), nil
}

func printEnrollment(w io.Writer, e auth.Enrollment) {
	_, _ = fmt.Fprintln(w, "\nScan this QR code with your authenticator app:")
	if qr, err := qrcode.New(e.URI, qrcode.Medium); err == nil {
		_, _ = fmt.Fprintln(w, qr.ToSmallString(false))
	}
	_, _ = fmt.Fprintf(w, "Or enter the secret manually: %s\n\n", e.Secret)
	_, _ = fmt.Fprintln(w, "Recovery codes (each works once, shown only now; store them offline):")
	for _, c := range e.RecoveryCodes {
		_, _ = fmt.Fprintf(w, "  %s\n", c)
	}
}
