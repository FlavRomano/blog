package server_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/FlavRomano/blog/internal/server"
	"github.com/FlavRomano/blog/internal/turnstile"
)

// commentEnv is an admin env with one published post, "hello".
type commentEnv struct {
	*adminEnv
	csrf string
}

func newCommentEnv(t *testing.T, opts ...func(*server.Deps)) *commentEnv {
	t.Helper()
	e := &commentEnv{adminEnv: newAdminEnv(t, opts...)}
	e.csrf = e.login(t)
	resp, body := e.postForm(t, "/admin/posts", postValues(e.csrf))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create post: %d %s", resp.StatusCode, body)
	}
	return e
}

// say posts a comment from the given client IP and returns the response.
func (e *commentEnv) say(t *testing.T, ip string, kv ...string) (*http.Response, string) {
	t.Helper()
	v := url.Values{"name": {"Ann"}, "body": {"Nice post"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return e.do(t, "POST", "/posts/hello/comments", strings.NewReader(v.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded", "CF-Connecting-IP": ip})
}

func (e *commentEnv) rows(t *testing.T) (depths, parents []int64) {
	t.Helper()
	rows, err := e.q.ListCommentsByPost(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		depths = append(depths, r.Depth)
		parents = append(parents, r.ParentID.Int64)
	}
	return depths, parents
}

func TestCommentPostAndRender(t *testing.T) {
	e := newCommentEnv(t)
	resp, body := e.say(t, "1.1.1.1", "body", "hi <b>there</b>\n\n\n\nsecond")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/posts/hello#comment-1" {
		t.Fatalf("post: %d %s %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	_, page := e.get(t, "/posts/hello")
	for _, want := range []string{"1 comment", "<strong>Ann</strong>", "hi &lt;b&gt;there&lt;/b&gt;\n\nsecond", `id="comment-1"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "<b>there") {
		t.Error("comment HTML not escaped")
	}
}

func TestCommentNestingDepthCap(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "1.1.1.1") // 1, depth 1
	for i, parent := range []int{1, 2, 3, 4, 5} {
		ip := "2.2.2." + strconv.Itoa(i+1)
		resp, body := e.say(t, ip, "parent", strconv.Itoa(parent))
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("reply to %d: %d %s", parent, resp.StatusCode, body)
		}
	}
	depths, parents := e.rows(t)
	wantDepths := []int64{1, 2, 3, 4, 4, 4}
	wantParents := []int64{0, 1, 2, 3, 3, 3}
	for i := range wantDepths {
		if depths[i] != wantDepths[i] || parents[i] != wantParents[i] {
			t.Fatalf("depths %v parents %v, want %v %v", depths, parents, wantDepths, wantParents)
		}
	}
}

func TestCommentReplyForm(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "1.1.1.1")
	_, page := e.get(t, "/posts/hello?reply=1")
	if !strings.Contains(page, `name="parent" value="1"`) || !strings.Contains(page, "Reply to Ann") {
		t.Error("reply form not prefilled")
	}
	_, page = e.get(t, "/posts/hello?reply=99")
	if strings.Contains(page, `name="parent"`) {
		t.Error("reply to unknown comment should be ignored")
	}
}

func TestCommentValidation(t *testing.T) {
	e := newCommentEnv(t)
	cases := map[string][]string{
		"no name":      {"name", "  "},
		"long name":    {"name", strings.Repeat("a", 51)},
		"author name":  {"name", "t"},
		"short body":   {"body", "x"},
		"long body":    {"body", strings.Repeat("a", 2001)},
		"three links":  {"body", "http://a.com http://b.com www.c.com"},
		"unknown post": {"parent", "999"},
	}
	for name, kv := range cases {
		resp, _ := e.say(t, "3.3.3.3", kv...)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: got %d", name, resp.StatusCode)
		}
	}
	if resp, _ := e.say(t, "3.3.3.3", "body", "http://a.com and http://b.com"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("two links should pass: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "POST", "/posts/nope/comments", strings.NewReader(""), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown post: %d", resp.StatusCode)
	}
}

func TestCommentHoneypot(t *testing.T) {
	e := newCommentEnv(t)
	resp, _ := e.say(t, "1.1.1.1", "website", "http://spam")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("honeypot status %d", resp.StatusCode)
	}
	if d, _ := e.rows(t); len(d) != 0 {
		t.Fatal("honeypot comment was stored")
	}
}

func TestCommentRateLimit(t *testing.T) {
	e := newCommentEnv(t)
	for i := 0; i < 3; i++ {
		if resp, _ := e.say(t, "4.4.4.4"); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("comment %d: %d", i, resp.StatusCode)
		}
	}
	resp, _ := e.say(t, "4.4.4.4")
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("4th comment: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "5.5.5.5"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("other IP: %d", resp.StatusCode)
	}
}

func TestCommentBanAndUnban(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "6.6.6.6")
	if resp, _ := e.postForm(t, "/admin/comments/1/ban", url0(e.csrf)); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("ban: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "6.6.6.6"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("banned IP: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "7.7.7.7"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("other IP: %d", resp.StatusCode)
	}
	_, page := e.get(t, "/admin/comments")
	bans, _ := e.q.ListBans(t.Context())
	if len(bans) != 1 || !strings.Contains(page, "Unban") {
		t.Fatalf("bans %v", bans)
	}
	if resp, _ := e.postForm(t, "/admin/bans/"+bans[0].IpHash+"/delete", url0(e.csrf)); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unban: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "6.6.6.6"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("after unban: %d", resp.StatusCode)
	}
	if resp, _ := e.postForm(t, "/admin/bans/not-a-hash/delete", url0(e.csrf)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bad hash: %d", resp.StatusCode)
	}
}

func url0(csrf string) url.Values { return url.Values{"csrf": {csrf}} }

func TestCommentDeleteTombstone(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "1.1.1.1", "body", "parent text")
	e.say(t, "2.2.2.2", "body", "child text", "parent", "1")

	e.postForm(t, "/admin/comments/1/delete", url0(e.csrf))
	_, page := e.get(t, "/posts/hello")
	if !strings.Contains(page, "[deleted]") || !strings.Contains(page, "child text") || strings.Contains(page, "parent text") {
		t.Fatal("parent should be a tombstone with its reply kept")
	}
	if !strings.Contains(page, "1 comment") {
		t.Error("tombstones should not count")
	}
	c, _ := e.q.GetComment(t.Context(), 1)
	if c.IpHash != "" || c.Body != "" || c.Author != "" {
		t.Error("tombstone kept personal data")
	}

	e.postForm(t, "/admin/comments/2/delete", url0(e.csrf))
	if d, _ := e.rows(t); len(d) != 0 {
		t.Fatalf("tombstone without replies should vanish, %d left", len(d))
	}
}

func TestCommentRequiresAdminToModerate(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "1.1.1.1")
	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, p := range []string{"/admin/comments/1/delete", "/admin/comments/1/ban"} {
		resp, err := anon.PostForm(e.ts.URL+p, url.Values{}) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	if d, _ := e.rows(t); len(d) != 1 {
		t.Fatal("anonymous delete went through")
	}
}

func TestCommentUnseenBadge(t *testing.T) {
	e := newCommentEnv(t)
	e.say(t, "1.1.1.1")
	if _, page := e.get(t, "/admin"); !strings.Contains(page, "1 new") {
		t.Fatal("badge missing")
	}
	if _, page := e.get(t, "/admin/comments"); !strings.Contains(page, ">new<") {
		t.Fatal("new marker missing")
	}
	if _, page := e.get(t, "/admin"); strings.Contains(page, "1 new") {
		t.Fatal("badge should clear after viewing")
	}
}

func TestCommentTurnstileAndNotify(t *testing.T) {
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ok := r.PostFormValue("response") == "good"
		_, _ = w.Write([]byte(`{"success":` + strconv.FormatBool(ok) + `}`)) //nolint:gosec // test server
	}))
	defer verify.Close()
	var mu sync.Mutex
	var notes []string
	e := newCommentEnv(t, func(d *server.Deps) {
		d.Cfg.TurnstileSiteKey = "site-key"
		d.Turnstile = &turnstile.Verifier{Secret: "x", URL: verify.URL}
		d.Notify = func(title, msg, link string) {
			mu.Lock()
			defer mu.Unlock()
			notes = append(notes, title+"|"+msg+"|"+link)
		}
	})
	if _, page := e.get(t, "/posts/hello"); !strings.Contains(page, `data-sitekey="site-key"`) {
		t.Error("widget missing")
	}
	if resp, _ := e.say(t, "1.1.1.1"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "1.1.1.1", "cf-turnstile-response", "bad"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("bad token: %d", resp.StatusCode)
	}
	if resp, _ := e.say(t, "1.1.1.1", "cf-turnstile-response", "good"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("good token: %d", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 1 || notes[0] != "New comment on Hello|Ann: Nice post|https://example.com/admin/comments" {
		t.Errorf("notes %q", notes)
	}
}

func TestCommentsDisabledWithoutIP(t *testing.T) {
	e := newAdminEnv(t, func(d *server.Deps) { d.Auth, d.IP = nil, nil })
	resp, _ := e.do(t, "POST", "/posts/x/comments", strings.NewReader(""), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
