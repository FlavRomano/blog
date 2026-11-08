package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/FlavRomano/blog/web"
)

type assets struct {
	static  fs.FS
	chroma  []byte
	version string
}

func loadAssets() (*assets, error) {
	sub, err := fs.Sub(web.FS, "static")
	if err != nil {
		return nil, err
	}
	css, err := chromaCSS()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(css)
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		h.Write([]byte(p))
		h.Write(b)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &assets{static: sub, chroma: css, version: hex.EncodeToString(h.Sum(nil))[:10]}, nil
}

// chromaCSS emits class-based highlight rules: github for light, dracula scoped under .dark.
func chromaCSS() ([]byte, error) {
	f := html.New(html.WithClasses(true))
	var light, dark bytes.Buffer
	if err := f.WriteCSS(&light, styles.Get("github")); err != nil {
		return nil, err
	}
	if err := f.WriteCSS(&dark, styles.Get("dracula")); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString(":root{--chroma-bg:#fff}.dark{--chroma-bg:#282a36}\n")
	out.Write(light.Bytes())
	out.WriteString(strings.ReplaceAll(dark.String(), ".chroma", ".dark .chroma"))
	return out.Bytes(), nil
}

func (a *assets) handler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(a.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if r.URL.Path == "/static/chroma.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write(a.chroma)
			return
		}
		files.ServeHTTP(w, r)
	})
}
