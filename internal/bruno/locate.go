// Package bruno locates a Bruno collection on disk.
package bruno

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const marker = "bruno.json"

// ErrNotFound is returned when no collection could be located.
var ErrNotFound = errors.New("no se encontró ninguna colección Bruno (se buscó ./bruno/bruno.json o bruno.json en este directorio y sus padres)")

// FindCollection returns the absolute path of the Bruno collection directory.
//
// If override is set it must point to a collection directory. Otherwise it
// walks up from start looking for either start/bruno.json or
// start/bruno/bruno.json.
func FindCollection(start, override string) (string, error) {
	if override != "" {
		dir, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if !isFile(filepath.Join(dir, marker)) {
			return "", fmt.Errorf("%s no es una colección Bruno (falta %s)", dir, marker)
		}
		return dir, nil
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if isFile(filepath.Join(dir, marker)) {
			return dir, nil
		}
		if candidate := filepath.Join(dir, "bruno"); isFile(filepath.Join(candidate, marker)) {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
