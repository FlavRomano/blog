package turnstile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerify(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("secret") != "s3" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"success":` + map[bool]string{true: "true", false: "false"}[r.PostFormValue("response") == "good"] + `}`))
	}))
	defer ts.Close()
	v := &Verifier{Secret: "s3", URL: ts.URL}
	ctx := context.Background()
	if ok, err := v.Verify(ctx, "good", "1.2.3.4"); !ok || err != nil {
		t.Fatalf("good: %v %v", ok, err)
	}
	if ok, err := v.Verify(ctx, "bad", ""); ok || err != nil {
		t.Fatalf("bad: %v %v", ok, err)
	}
	if ok, err := v.Verify(ctx, "", ""); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	v.Secret = "wrong"
	if _, err := v.Verify(ctx, "good", ""); err == nil {
		t.Fatal("expected error on non-200")
	}
}
