package bruno

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func mkCollection(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, marker), []byte(`{"version":"1","name":"x","type":"collection"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFindCollection(t *testing.T) {
	repo := resolved(t, t.TempDir())
	coll := filepath.Join(repo, "bruno")
	mkCollection(t, coll)
	sub := filepath.Join(repo, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"repo root":      repo,
		"inside bruno":   coll,
		"deep subfolder": sub,
	}
	for name, start := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := FindCollection(start, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != coll {
				t.Fatalf("got %s, want %s", got, coll)
			}
		})
	}
}

func TestFindCollectionOverride(t *testing.T) {
	dir := resolved(t, t.TempDir())
	other := filepath.Join(dir, "api-collection")
	mkCollection(t, other)

	got, err := FindCollection(dir, other)
	if err != nil {
		t.Fatal(err)
	}
	if got != other {
		t.Fatalf("got %s, want %s", got, other)
	}

	if _, err := FindCollection(dir, dir); err == nil {
		t.Fatal("expected error for override without bruno.json")
	}
}

func TestFindCollectionNotFound(t *testing.T) {
	_, err := FindCollection(t.TempDir(), "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
