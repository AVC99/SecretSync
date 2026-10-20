// Package fsutil contains small file system helpers.
package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile atomically replaces path with data, creating parent directories
// as needed. New files get newPerm; existing files keep their permissions.
func WriteFile(path string, data []byte, newPerm fs.FileMode) error {
	mode := newPerm
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".secretsync-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
