package bruno

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Environment is a Bruno environment file (environments/<name>.bru).
type Environment struct {
	Name string
	Path string
}

// EnvironmentPath returns the path of the environment called name.
func EnvironmentPath(collection, name string) string {
	return filepath.Join(collection, "environments", name+".bru")
}

var envNameRe = regexp.MustCompile(`^[\w. -]+$`)

// ValidEnvironmentName reports whether name can be used as an environment file name.
func ValidEnvironmentName(name string) bool {
	return envNameRe.MatchString(name) && strings.TrimSpace(name) == name
}

// ListEnvironments returns the environments of a collection, sorted by name.
func ListEnvironments(collection string) ([]Environment, error) {
	paths, err := filepath.Glob(filepath.Join(collection, "environments", "*.bru"))
	if err != nil {
		return nil, err
	}
	envs := make([]Environment, 0, len(paths))
	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			continue
		}
		envs = append(envs, Environment{Name: strings.TrimSuffix(filepath.Base(p), ".bru"), Path: p})
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	return envs, nil
}

// ProcessEnvRef is the Bruno expression that reads key from the collection's .env.
func ProcessEnvRef(key string) string {
	return "{{process.env." + key + "}}"
}

var (
	varsStartRe   = regexp.MustCompile(`^vars\s*\{\s*$`)
	secretStartRe = regexp.MustCompile(`^vars:secret\s*\[`)
	varLineRe     = regexp.MustCompile(`^(\s*)~?([^\s:]+)\s*:\s?(.*)$`)
)

// EnvFile is a parsed Bruno environment file. Only the `vars { }` and
// `vars:secret [ ]` blocks are interpreted; everything else is preserved.
type EnvFile struct {
	lines []string
}

// ParseEnv parses the content of an environment .bru file.
func ParseEnv(data []byte) *EnvFile {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return &EnvFile{}
	}
	return &EnvFile{lines: strings.Split(text, "\n")}
}

// varsBlock returns the indexes of the "vars {" and closing "}" lines, or -1, -1.
func (e *EnvFile) varsBlock() (start, end int) {
	for i, l := range e.lines {
		if !varsStartRe.MatchString(l) {
			continue
		}
		inMultiline := false
		for j := i + 1; j < len(e.lines); j++ {
			t := strings.TrimSpace(e.lines[j])
			if strings.Contains(t, "'''") && strings.Count(t, "'''")%2 == 1 {
				inMultiline = !inMultiline
				continue
			}
			if !inMultiline && t == "}" {
				return i, j
			}
		}
		return -1, -1
	}
	return -1, -1
}

// entries maps each variable of the vars block to its line index.
func (e *EnvFile) entries() map[string]int {
	out := map[string]int{}
	start, end := e.varsBlock()
	if start < 0 {
		return out
	}
	inMultiline := false
	for i := start + 1; i < end; i++ {
		l := e.lines[i]
		if inMultiline {
			if strings.Contains(l, "'''") {
				inMultiline = false
			}
			continue
		}
		m := varLineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		out[m[2]] = i
		if strings.TrimSpace(m[3]) == "'''" {
			inMultiline = true
		}
	}
	return out
}

// Lookup returns the value of a (possibly disabled) variable in the vars block.
func (e *EnvFile) Lookup(key string) (string, bool) {
	i, ok := e.entries()[key]
	if !ok {
		return "", false
	}
	m := varLineRe.FindStringSubmatch(e.lines[i])
	return strings.TrimSpace(m[3]), true
}

// Secrets returns the names declared in the vars:secret block.
func (e *EnvFile) Secrets() []string {
	var names []string
	for i, l := range e.lines {
		if !secretStartRe.MatchString(l) {
			continue
		}
		body := strings.TrimSpace(secretStartRe.ReplaceAllString(l, ""))
		for j := i + 1; !strings.Contains(body, "]") && j < len(e.lines); j++ {
			body += "\n" + e.lines[j]
		}
		body, _, _ = strings.Cut(body, "]")
		for _, f := range strings.FieldsFunc(body, func(r rune) bool { return r == ',' || r == '\n' }) {
			if n := strings.TrimPrefix(strings.TrimSpace(f), "~"); n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

// SetVars sets each variable in the vars block, replacing existing
// definitions (and re-enabling disabled ones) and appending new ones. The
// block is created at the top of the file if missing.
func (e *EnvFile) SetVars(vars map[string]string) {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	_, end := e.varsBlock()
	if end < 0 {
		block := []string{"vars {", "}"}
		if len(e.lines) > 0 {
			block = append(block, "")
		}
		e.lines = append(block, e.lines...)
		end = 1
	}

	existing := e.entries()
	var added []string
	for _, k := range keys {
		line := "  " + k + ": " + vars[k]
		if i, ok := existing[k]; ok {
			indent := varLineRe.FindStringSubmatch(e.lines[i])[1]
			e.lines[i] = indent + k + ": " + vars[k]
			continue
		}
		added = append(added, line)
	}
	if len(added) > 0 {
		lines := make([]string, 0, len(e.lines)+len(added))
		lines = append(lines, e.lines[:end]...)
		lines = append(lines, added...)
		lines = append(lines, e.lines[end:]...)
		e.lines = lines
	}
}

// Bytes renders the file.
func (e *EnvFile) Bytes() []byte {
	if len(e.lines) == 0 {
		return nil
	}
	return []byte(strings.Join(e.lines, "\n") + "\n")
}
