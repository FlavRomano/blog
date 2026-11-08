package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxUploadBytes = 5 << 20

var imageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var extTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

var uploadName = regexp.MustCompile(`^[0-9a-f]{32}\.(?:png|jpg|gif|webp)$`)

// imageUpload stores the file as-is under a random name. The type is decided by
// sniffing content, never by the client-supplied name or header; SVG is refused.
func (s *Server) imageUpload(w http.ResponseWriter, r *http.Request) {
	reply := func(status int, msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `<p role="alert" class="text-sm text-red-600 dark:text-red-400">%s</p>`, html.EscapeString(msg))
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reply(http.StatusRequestEntityTooLarge, "Image is too large (max 5 MB).")
			return
		}
		reply(http.StatusBadRequest, "Choose an image to upload.")
		return
	}
	defer func() { _ = file.Close() }()
	if hdr.Size > maxUploadBytes {
		reply(http.StatusRequestEntityTooLarge, "Image is too large (max 5 MB).")
		return
	}

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	ext, ok := imageTypes[http.DetectContentType(head[:n])]
	if !ok {
		reply(http.StatusUnsupportedMediaType, "Only PNG, JPEG, GIF and WebP images are accepted.")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		s.serverError(w, "seek upload", err)
		return
	}

	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		s.serverError(w, "random name", err)
		return
	}
	name := hex.EncodeToString(rnd[:]) + ext
	if err := os.MkdirAll(s.cfg.UploadsDir, 0o750); err != nil {
		s.serverError(w, "mkdir uploads", err)
		return
	}
	path := filepath.Join(s.cfg.UploadsDir, name)
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec // path is built from a random hex name
	if err != nil {
		s.serverError(w, "create upload", err)
		return
	}
	_, copyErr := io.Copy(out, io.LimitReader(file, maxUploadBytes))
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(path)
		s.serverError(w, "write upload", copyErr)
		return
	}

	alt := strings.TrimSpace(regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename)), " "))
	snippet := fmt.Sprintf("![%s](/uploads/%s)", alt, name)
	s.log.Info("image uploaded", "name", name, "bytes", hdr.Size)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<p class="text-sm"><button type="button" data-insert="%s" class="btn">Insert into post</button> <code class="break-all">%s</code></p>`,
		html.EscapeString(snippet), html.EscapeString(snippet))
}

// upload serves stored images. Only names this app generated are served.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !uploadName.MatchString(name) {
		s.notFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.UploadsDir, name)) //nolint:gosec // name validated by regexp
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", extTypes[filepath.Ext(name)])
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, st.ModTime(), f)
}
