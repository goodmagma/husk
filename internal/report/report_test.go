package report

import (
	"path/filepath"
	"testing"
)

func TestNewRunDir(t *testing.T) {
	parent := t.TempDir()
	want := []string{"husk_x", "husk_x_2", "husk_x_3"}
	for _, name := range want {
		dir, err := newRunDir(parent, "husk_x")
		if err != nil {
			t.Fatal(err)
		}
		if got := filepath.Base(dir); got != name {
			t.Errorf("got %s, want %s", got, name)
		}
	}
}
