package dotenv

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	src := "# comment\n" +
		"\n" +
		"PLAIN=value\n" +
		"SPACED =  padded value   # trailing comment\n" +
		"export EXPORTED=yes\n" +
		"SINGLE='literal \\n #not comment'\n" +
		"DOUBLE=\"line1\\nline2\"\n" +
		"BACKTICK=`has 'single' and \"double\"`\n" +
		"MULTI=\"first\n" +
		"second\"\n" +
		"EMPTY=\n" +
		"DUP=one\n" +
		"DUP=two\n" +
		"not an assignment\n" +
		"UNTERMINATED=\"oops\n"

	f := Parse([]byte(src))
	want := map[string]string{
		"PLAIN":        "value",
		"SPACED":       "padded value",
		"EXPORTED":     "yes",
		"SINGLE":       `literal \n #not comment`,
		"DOUBLE":       "line1\nline2",
		"BACKTICK":     `has 'single' and "double"`,
		"MULTI":        "first\nsecond",
		"EMPTY":        "",
		"DUP":          "two",
		"UNTERMINATED": `"oops`,
	}
	for k, v := range want {
		got, ok := f.Lookup(k)
		if !ok || got != v {
			t.Errorf("Lookup(%q) = %q, %v; want %q", k, got, ok, v)
		}
	}
	if string(f.Bytes()) != src {
		t.Errorf("round trip changed the file:\n%s", f.Bytes())
	}
}

func TestParseCRLF(t *testing.T) {
	f := Parse([]byte("A=1\r\nB='x'\r\n"))
	if v, _ := f.Lookup("A"); v != "1" {
		t.Errorf("A = %q", v)
	}
	if v, _ := f.Lookup("B"); v != "x" {
		t.Errorf("B = %q", v)
	}
}

func TestMerge(t *testing.T) {
	src := "# Bruno env\n" +
		"KEEP=same\n" +
		"export TOKEN=old # rotate me\n" +
		"OTHER=untouched\n"
	f := Parse([]byte(src))

	vars := map[string]string{
		"KEEP":    "same",
		"TOKEN":   "new token",
		"NEW_ONE": "a#b",
		"NEW_TWO": "plain",
	}
	c := f.Diff(vars)
	wantChanges := Changes{
		Added:     []string{"NEW_ONE", "NEW_TWO"},
		Updated:   []string{"TOKEN"},
		Unchanged: []string{"KEEP"},
	}
	if !reflect.DeepEqual(c, wantChanges) {
		t.Fatalf("Diff() = %+v, want %+v", c, wantChanges)
	}

	f.Merge(vars, "added by secretsync")
	want := "# Bruno env\n" +
		"KEEP=same\n" +
		"export TOKEN='new token'\n" +
		"OTHER=untouched\n" +
		"\n" +
		"# added by secretsync\n" +
		"NEW_ONE='a#b'\n" +
		"NEW_TWO=plain\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("Merge result:\n%s\nwant:\n%s", got, want)
	}

	// Merging again is a no-op.
	f2 := Parse(f.Bytes())
	if c := f2.Diff(vars); len(c.Added)+len(c.Updated) != 0 {
		t.Fatalf("second Diff should be empty, got %+v", c)
	}
	f2.Merge(vars, "added by secretsync")
	if string(f2.Bytes()) != want {
		t.Fatalf("second Merge changed the file:\n%s", f2.Bytes())
	}
}

func TestMergeEmptyFile(t *testing.T) {
	f := Parse(nil)
	f.Merge(map[string]string{"A": "1"}, "hdr")
	if got, want := string(f.Bytes()), "# hdr\nA=1\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestQuoteRoundTrip(t *testing.T) {
	values := []string{
		"",
		"simple",
		"with space",
		"hash#tag",
		"it's",
		`say "hi"`,
		"it's `both`",
		"multi\nline",
		`C:\path\to`,
		`back\slash end\`,
		`{"json":"value","n":1}`,
		"  leading and trailing  ",
		"postgres://user:p@ss@host:5432/db?sslmode=require",
	}
	for _, v := range values {
		line := "K=" + Quote(v) + "\n"
		got, ok := Parse([]byte(line)).Lookup("K")
		if !ok || got != v {
			t.Errorf("value %q written as %q read back as %q", v, line, got)
		}
	}
}

func TestValidKey(t *testing.T) {
	for k, want := range map[string]bool{
		"API_KEY": true, "api.key": true, "api-key": true, "a b": false, "": false, "a=b": false,
	} {
		if ValidKey(k) != want {
			t.Errorf("ValidKey(%q) != %v", k, want)
		}
	}
}

func TestKeys(t *testing.T) {
	f := Parse([]byte("# c\nB=1\nA=2\nB=3\n"))
	if got := f.Keys(); !reflect.DeepEqual(got, []string{"B", "A"}) {
		t.Fatalf("Keys() = %v", got)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Keys()) != 0 {
		t.Fatal("expected empty file")
	}
}
