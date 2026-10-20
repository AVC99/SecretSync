// Package dotenv reads and merges .env files while preserving comments,
// ordering and untouched lines.
//
// Parsing follows the semantics of the npm "dotenv" package, which is what
// Bruno uses to load a collection's .env file:
//   - 'single' and `backtick` quoted values are literal and may span lines;
//   - "double" quoted values may span lines and expand \n and \r;
//   - unquoted values end at the first '#' and are trimmed.
package dotenv

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	assignRe = regexp.MustCompile(`^\s*(export\s+)?([\w.-]+)\s*=\s*(.*)$`)
	keyRe    = regexp.MustCompile(`^[\w.-]+$`)
)

// ValidKey reports whether key can be used as a variable name in a .env file.
func ValidKey(key string) bool { return keyRe.MatchString(key) }

type entry struct {
	raw    string // original text, may span several lines (no trailing newline)
	key    string // empty for comments, blank lines and unparseable lines
	value  string
	export bool
}

// File is a parsed .env file.
type File struct {
	entries []entry
}

// Parse parses the content of a .env file. It never fails: lines it does not
// understand are preserved verbatim.
func Parse(data []byte) *File {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	f := &File{}
	if text == "" {
		return f
	}
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := assignRe.FindStringSubmatch(lines[i])
		if m == nil {
			f.entries = append(f.entries, entry{raw: lines[i]})
			continue
		}
		e := entry{raw: lines[i], key: m[2], export: m[1] != ""}
		rest := m[3]

		if q := quoteOf(rest); q != 0 {
			// Look for the closing quote, possibly on following lines.
			body := rest[1:]
			consumed := 0
			for {
				if end := closingQuote(body, q); end >= 0 {
					e.value = unquote(body[:end], q)
					e.raw = strings.Join(lines[i:i+consumed+1], "\n")
					i += consumed
					break
				}
				if i+consumed+1 >= len(lines) {
					// Unterminated: treat the first line as an unquoted value.
					e.value = unquoted(rest)
					break
				}
				consumed++
				body += "\n" + lines[i+consumed]
			}
		} else {
			e.value = unquoted(rest)
		}
		f.entries = append(f.entries, e)
	}
	return f
}

func quoteOf(s string) byte {
	if s != "" && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		return s[0]
	}
	return 0
}

// closingQuote returns the index of the first unescaped q in s, or -1.
func closingQuote(s string, q byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == q {
			i++
			continue
		}
		if s[i] == q {
			return i
		}
	}
	return -1
}

func unquote(s string, q byte) string {
	if q == '"' {
		s = strings.ReplaceAll(s, `\n`, "\n")
		s = strings.ReplaceAll(s, `\r`, "\r")
	}
	return s
}

func unquoted(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Lookup returns the effective value of key (the last occurrence wins, as in dotenv).
func (f *File) Lookup(key string) (string, bool) {
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].key == key {
			return f.entries[i].value, true
		}
	}
	return "", false
}

// Changes summarizes what Merge would do.
type Changes struct {
	Added, Updated, Unchanged []string
}

// Diff reports which keys of vars are new, different or identical.
func (f *File) Diff(vars map[string]string) Changes {
	return Diff(f.Lookup, vars)
}

// Diff classifies the keys of vars against the current values returned by lookup.
func Diff(lookup func(string) (string, bool), vars map[string]string) Changes {
	var c Changes
	for _, k := range sortedKeys(vars) {
		old, ok := lookup(k)
		switch {
		case !ok:
			c.Added = append(c.Added, k)
		case old != vars[k]:
			c.Updated = append(c.Updated, k)
		default:
			c.Unchanged = append(c.Unchanged, k)
		}
	}
	return c
}

// Keys returns the variable names in order of first appearance.
func (f *File) Keys() []string {
	seen := map[string]bool{}
	var keys []string
	for _, e := range f.entries {
		if e.key != "" && !seen[e.key] {
			seen[e.key] = true
			keys = append(keys, e.key)
		}
	}
	return keys
}

// Merge updates existing keys in place and appends new ones at the end of
// the file, after a comment line containing header (if not empty).
func (f *File) Merge(vars map[string]string, header string) {
	f.MergeOrdered(vars, nil, header)
}

// MergeOrdered is like Merge but appends new keys following order; keys not
// in order are appended afterwards, sorted.
func (f *File) MergeOrdered(vars map[string]string, order []string, header string) {
	keys := make([]string, 0, len(vars))
	seen := map[string]bool{}
	for _, k := range order {
		if _, ok := vars[k]; ok && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for _, k := range sortedKeys(vars) {
		if !seen[k] {
			keys = append(keys, k)
		}
	}

	var added []string
	for _, k := range keys {
		v := vars[k]
		found := false
		for i := range f.entries {
			e := &f.entries[i]
			if e.key != k {
				continue
			}
			found = true
			if e.value != v {
				e.value = v
				e.raw = render(e.export, k, v)
			}
		}
		if !found {
			added = append(added, k)
		}
	}
	if len(added) == 0 {
		return
	}
	if n := len(f.entries); n > 0 && strings.TrimSpace(f.entries[n-1].raw) != "" {
		f.entries = append(f.entries, entry{raw: ""})
	}
	if header != "" {
		f.entries = append(f.entries, entry{raw: "# " + header})
	}
	for _, k := range added {
		f.entries = append(f.entries, entry{raw: render(false, k, vars[k]), key: k, value: vars[k]})
	}
}

// Bytes renders the file.
func (f *File) Bytes() []byte {
	var b bytes.Buffer
	for _, e := range f.entries {
		b.WriteString(e.raw)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func render(export bool, key, value string) string {
	prefix := ""
	if export {
		prefix = "export "
	}
	return prefix + key + "=" + Quote(value)
}

// Quote returns value written so that dotenv reads it back unchanged.
func Quote(v string) string {
	// Unquoted values are cut at '#' and trimmed, so they are only safe
	// without those; we still prefer quotes for anything containing blanks
	// or quote characters, for readability.
	safeUnquoted := !strings.ContainsAny(v, "#\n\r") && strings.TrimSpace(v) == v && quoteOf(v) == 0
	if safeUnquoted && !strings.ContainsAny(v, "'\"` \t") {
		return v
	}
	// A trailing backslash would escape the closing quote.
	if !strings.HasSuffix(v, `\`) {
		for _, q := range []string{"'", "`"} {
			if !strings.Contains(v, q) {
				return q + v + q
			}
		}
		if !strings.Contains(v, `"`) && !strings.Contains(v, `\n`) && !strings.Contains(v, `\r`) {
			return `"` + v + `"`
		}
	}
	if safeUnquoted {
		return v
	}
	// No quoting style round-trips this value exactly; double quotes are the
	// closest approximation.
	v = strings.ReplaceAll(v, "\r", `\r`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return `"` + v + `"`
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Load reads and parses path. A missing file yields an empty File.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data), nil
}
