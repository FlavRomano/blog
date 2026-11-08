package server_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/clientip"
	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/server"
)

const adminPassword = "correct horse battery"

type adminEnv struct {
	ts      *httptest.Server
	client  *http.Client
	q       *sqlc.Queries
	enr     auth.Enrollment
	now     *time.Time
	uploads string
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	keys, err := auth.NewKeys(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(db, keys, auth.Options{Issuer: "Test", Now: func() time.Time { return now }})
	enr, err := svc.Enroll(context.Background(), adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	e := &adminEnv{q: sqlc.New(db), enr: enr, now: &now, uploads: t.TempDir()}
	h, err := server.New(server.Deps{
		DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:  config.Config{BaseURL: "https://example.com", SiteName: "Test Site", AuthorName: "T", UploadsDir: e.uploads, Timezone: "Europe/Rome"},
		Auth: svc, IP: clientip.New("salt", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ts = httptest.NewServer(h)
	t.Cleanup(e.ts.Close)
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return e
}

func (e *adminEnv) code() string {
	secret, _ := auth.DecodeSecret(e.enr.Secret)
	return auth.CodeAt(secret, *e.now)
}

func (e *adminEnv) do(t *testing.T, method, path string, body io.Reader, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func (e *adminEnv) get(t *testing.T, path string) (*http.Response, string) {
	return e.do(t, "GET", path, nil, nil)
}

func (e *adminEnv) postForm(t *testing.T, path string, v url.Values) (*http.Response, string) {
	return e.do(t, "POST", path, strings.NewReader(v.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

// login signs in and returns the session CSRF token.
func (e *adminEnv) login(t *testing.T) string {
	t.Helper()
	*e.now = e.now.Add(time.Minute) // fresh TOTP step each time
	resp, body := e.postForm(t, "/admin/login", url.Values{"password": {adminPassword}, "code": {e.code()}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
	return e.csrf(t)
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (e *adminEnv) csrf(t *testing.T) string {
	t.Helper()
	_, body := e.get(t, "/admin")
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf token in dashboard: %s", body)
	}
	return m[1]
}

func postValues(csrf string, kv ...string) url.Values {
	v := url.Values{"csrf": {csrf}, "title": {"Hello"}, "body": {"Some **body**"}, "status": {"published"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func TestAdminRequiresLogin(t *testing.T) {
	e := newAdminEnv(t)
	for _, p := range []string{"/admin", "/admin/", "/admin/posts/new", "/admin/posts/1"} {
		resp, _ := e.get(t, p)
		if resp.StatusCode != 303 || resp.Header.Get("Location") != "/admin/login" {
			t.Errorf("%s: %d %s", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	resp, _ := e.postForm(t, "/admin/preview", url.Values{"body": {"x"}})
	if resp.StatusCode != 303 {
		t.Errorf("unauthenticated POST: %d", resp.StatusCode)
	}
	resp, _ = e.do(t, "POST", "/admin/preview", nil, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 401 || resp.Header.Get("HX-Redirect") != "/admin/login" {
		t.Errorf("htmx unauthenticated: %d", resp.StatusCode)
	}
}

func TestLoginLogout(t *testing.T) {
	e := newAdminEnv(t)

	resp, body := e.postForm(t, "/admin/login", url.Values{"password": {"wrong wrong wrong"}, "code": {e.code()}})
	if resp.StatusCode != 401 || !strings.Contains(body, "Invalid credentials") {
		t.Fatalf("bad login: %d", resp.StatusCode)
	}

	csrf := e.login(t)
	var cookie *http.Cookie
	for _, c := range e.client.Jar.Cookies(mustURL(e.ts.URL)) {
		cookie = c
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	resp, _ = e.get(t, "/admin")
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(resp.Header.Get("X-Robots-Tag"), "noindex") {
		t.Errorf("dashboard: %d %v", resp.StatusCode, resp.Header)
	}
	setCookie := loginSetCookie(t, e)
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Errorf("cookie flags: %s", setCookie)
	}

	resp, _ = e.postForm(t, "/admin/logout", url.Values{"csrf": {csrf}})
	if resp.StatusCode != 303 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if r, _ := e.get(t, "/admin"); r.StatusCode != 303 {
		t.Error("still logged in after logout")
	}
}

func loginSetCookie(t *testing.T, e *adminEnv) string {
	t.Helper()
	*e.now = e.now.Add(time.Minute)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"password": {adminPassword}, "code": {e.code()}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, e.ts.URL+"/admin/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.Header.Get("Set-Cookie")
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

func TestLoginRateLimited(t *testing.T) {
	e := newAdminEnv(t)
	for range 5 {
		e.postForm(t, "/admin/login", url.Values{"password": {"nope nope nope"}, "code": {"000000"}})
	}
	resp, _ := e.postForm(t, "/admin/login", url.Values{"password": {adminPassword}, "code": {e.code()}})
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestCSRFAndCrossSiteProtection(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)

	resp, _ := e.postForm(t, "/admin/posts", postValues(""))
	if resp.StatusCode != 403 {
		t.Errorf("missing csrf: %d", resp.StatusCode)
	}
	resp, _ = e.postForm(t, "/admin/posts", postValues("wrong"))
	if resp.StatusCode != 403 {
		t.Errorf("wrong csrf: %d", resp.StatusCode)
	}
	resp, _ = e.do(t, "POST", "/admin/posts", strings.NewReader(postValues(csrf).Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != 403 {
		t.Errorf("cross-site: %d", resp.StatusCode)
	}
	resp, _ = e.do(t, "POST", "/admin/login", strings.NewReader("password=x&code=1"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://evil.example"})
	if resp.StatusCode != 403 {
		t.Errorf("cross-origin login: %d", resp.StatusCode)
	}
	if n, _ := e.q.CountPublishedPosts(context.Background()); n != 0 {
		t.Error("post created despite rejection")
	}
}

func TestPostLifecycle(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)

	resp, body := e.postForm(t, "/admin/posts", postValues(csrf, "tags", "Go, SQLite, go", "summary", "A summary"))
	if resp.StatusCode != 303 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if loc != "/admin/posts/1?saved=1" {
		t.Fatalf("redirect to %q", loc)
	}
	if _, pub := e.get(t, "/posts/hello"); !strings.Contains(pub, "<strong>body</strong>") || !strings.Contains(pub, "#SQLite") {
		t.Errorf("published post not rendered: %s", pub)
	}
	if _, edit := e.get(t, loc); !strings.Contains(edit, "Saved.") || !strings.Contains(edit, `value="Go, SQLite"`) {
		t.Errorf("edit form: %s", edit)
	}

	resp, body = e.postForm(t, "/admin/posts/1", postValues(csrf, "status", "draft", "title", "Hello again", "slug", "hello", "tags", "SQLite"))
	if resp.StatusCode != 303 {
		t.Fatalf("update: %d %s", resp.StatusCode, body)
	}
	if r, _ := e.get(t, "/posts/hello"); r.StatusCode != 404 {
		t.Error("draft still public")
	}
	if _, tags := e.get(t, "/tags"); strings.Contains(tags, "#Go") || strings.Contains(tags, "SQLite") {
		t.Errorf("unpublished tags visible: %s", tags)
	}

	resp, _ = e.postForm(t, "/admin/posts/1/delete", url.Values{"csrf": {csrf}})
	if resp.StatusCode != 303 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if r, _ := e.get(t, "/admin/posts/1"); r.StatusCode != 404 {
		t.Error("deleted post still editable")
	}
	if tags, _ := e.q.ListTagsWithCounts(context.Background()); len(tags) != 0 {
		t.Error("orphan tags left")
	}
}

func TestPostValidation(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)
	future := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	past := time.Now().Add(-48 * time.Hour).Format("2006-01-02T15:04")

	cases := []struct {
		name string
		v    url.Values
		want string
	}{
		{"no title", postValues(csrf, "title", ""), "Title is required."},
		{"no body", postValues(csrf, "body", "  "), "Body is required."},
		{"bad status", postValues(csrf, "status", "weird"), "Choose a valid status."},
		{"scheduled without time", postValues(csrf, "status", "scheduled"), "Pick a publish time"},
		{"scheduled in past", postValues(csrf, "status", "scheduled", "publish_at", past), "must be in the future"},
		{"published in future", postValues(csrf, "publish_at", future), "scheduled status"},
		{"bad date", postValues(csrf, "publish_at", "tomorrow"), "Invalid date"},
		{"bad tag", postValues(csrf, "tags", "!!!"), "Tags must contain"},
	}
	for _, c := range cases {
		resp, body := e.postForm(t, "/admin/posts", c.v)
		if resp.StatusCode != 422 || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d, want %q", c.name, resp.StatusCode, c.want)
		}
	}

	if resp, _ := e.postForm(t, "/admin/posts", postValues(csrf)); resp.StatusCode != 303 {
		t.Fatal("first create failed")
	}
	resp, body := e.postForm(t, "/admin/posts", postValues(csrf))
	if resp.StatusCode != 422 || !strings.Contains(body, "slug is already used") {
		t.Errorf("duplicate slug: %d", resp.StatusCode)
	}
}

func TestScheduledPostPublishesWhenDue(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)
	future := time.Now().In(time.FixedZone("CEST", 2*3600)).Add(2 * time.Hour).Format("2006-01-02T15:04")

	resp, body := e.postForm(t, "/admin/posts", postValues(csrf, "status", "scheduled", "publish_at", future))
	if resp.StatusCode != 303 {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	if r, _ := e.get(t, "/posts/hello"); r.StatusCode != 404 {
		t.Fatal("scheduled post visible early")
	}
	p, _ := e.q.GetPostByID(context.Background(), 1)
	if p.Status != "scheduled" || !p.PublishedAt.Valid {
		t.Fatalf("stored: %+v", p)
	}
	// The scheduler uses the same query main wires up.
	n, err := e.q.PublishDuePosts(context.Background(), p.PublishedAt)
	if err != nil || n != 1 {
		t.Fatalf("publish due: %d %v", n, err)
	}
	if r, _ := e.get(t, "/posts/hello"); r.StatusCode != 200 {
		t.Error("post not public after publish")
	}
}

func TestPreviewSanitizes(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)
	resp, body := e.do(t, "POST", "/admin/preview", strings.NewReader(url.Values{"body": {"# Hi\n\n<script>alert(1)</script>"}}.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": csrf, "HX-Request": "true"})
	if resp.StatusCode != 200 || !strings.Contains(body, "<h1") || strings.Contains(body, "<script") {
		t.Errorf("preview: %d %s", resp.StatusCode, body)
	}
}

func upload(t *testing.T, e *adminEnv, csrf, filename string, data []byte) (*http.Response, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return e.do(t, "POST", "/admin/images", &buf, map[string]string{"Content-Type": mw.FormDataContentType(), "X-CSRF-Token": csrf})
}

func TestImageUpload(t *testing.T) {
	e := newAdminEnv(t)
	csrf := e.login(t)

	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 2, 2)))

	resp, body := upload(t, e, csrf, "My Photo.PNG", pngBuf.Bytes())
	if resp.StatusCode != 200 {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	m := regexp.MustCompile(`/uploads/([0-9a-f]{32}\.png)`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, "![My Photo](") {
		t.Fatalf("snippet: %s", body)
	}

	resp, served := e.get(t, "/uploads/"+m[1])
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("serve: %d %v", resp.StatusCode, resp.Header)
	}
	if served != pngBuf.String() {
		t.Error("file was modified")
	}

	for name, data := range map[string][]byte{
		"evil.png": []byte("<script>alert(1)</script>"),
		"x.svg":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>`),
		"a.html":   []byte("<html></html>"),
	} {
		if resp, _ := upload(t, e, csrf, name, data); resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("%s accepted: %d", name, resp.StatusCode)
		}
	}
	if resp, _ := upload(t, e, "", "a.png", pngBuf.Bytes()); resp.StatusCode != 403 {
		t.Errorf("upload without csrf: %d", resp.StatusCode)
	}
	big := append(append([]byte{}, pngBuf.Bytes()...), make([]byte, 6<<20)...)
	if resp, _ := upload(t, e, csrf, "big.png", big); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize: %d", resp.StatusCode)
	}
}

func TestUploadServingRejectsTraversalAndUnknownNames(t *testing.T) {
	e := newAdminEnv(t)
	for _, p := range []string{"/uploads/..%2fadmin", "/uploads/passwd", "/uploads/" + strings.Repeat("a", 32) + ".png", "/uploads/"} {
		if resp, _ := e.get(t, p); resp.StatusCode != 404 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
}

func TestAdminDisabledWithoutAuth(t *testing.T) {
	h, _ := newEnv(t)
	if rec := get(t, h, "/admin"); rec.Code != 404 {
		t.Errorf("admin should not exist: %d", rec.Code)
	}
}
