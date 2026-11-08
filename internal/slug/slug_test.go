package slug

import "testing"

func TestMake(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World!":    "hello-world",
		"  Crème brûlée  ": "creme-brulee",
		"Go & SQLite -- 1": "go-sqlite-1",
		"???":              "",
	} {
		if got := Make(in); got != want {
			t.Errorf("Make(%q)=%q want %q", in, got, want)
		}
	}
}
