package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", ".env")

	if err := WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("new file mode = %v, want 0600", info.Mode().Perm())
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	if err := WriteFile(path, []byte("A=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "A=2\n" {
		t.Fatalf("file = %q", data)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o640 {
			t.Errorf("existing mode not preserved: %v", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
