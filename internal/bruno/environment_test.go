package bruno

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sampleEnv = `vars {
  baseUrl: https://api.dev.example.com
  ~disabled: old
  notes: '''
    line: one
    }
  '''
  API_KEY: hardcoded
}
vars:secret [
  password,
  ~token
]
`

func TestEnvFileLookupAndSecrets(t *testing.T) {
	e := ParseEnv([]byte(sampleEnv))

	if v, ok := e.Lookup("baseUrl"); !ok || v != "https://api.dev.example.com" {
		t.Errorf("baseUrl = %q, %v", v, ok)
	}
	if v, ok := e.Lookup("disabled"); !ok || v != "old" {
		t.Errorf("disabled = %q, %v", v, ok)
	}
	if _, ok := e.Lookup("line"); ok {
		t.Error("line inside a multiline value parsed as a variable")
	}
	if _, ok := e.Lookup("password"); ok {
		t.Error("secret var reported as a plain var")
	}
	if got, want := e.Secrets(), []string{"password", "token"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Secrets() = %v, want %v", got, want)
	}
	if string(e.Bytes()) != sampleEnv {
		t.Errorf("round trip changed the file:\n%s", e.Bytes())
	}
}

func TestEnvFileSetVars(t *testing.T) {
	e := ParseEnv([]byte(sampleEnv))
	e.SetVars(map[string]string{
		"API_KEY":  ProcessEnvRef("API_KEY"),
		"disabled": ProcessEnvRef("disabled"),
		"DB_PASS":  ProcessEnvRef("DB_PASS"),
	})
	want := `vars {
  baseUrl: https://api.dev.example.com
  disabled: {{process.env.disabled}}
  notes: '''
    line: one
    }
  '''
  API_KEY: {{process.env.API_KEY}}
  DB_PASS: {{process.env.DB_PASS}}
}
vars:secret [
  password,
  ~token
]
`
	if got := string(e.Bytes()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEnvFileSetVarsWithoutVarsBlock(t *testing.T) {
	e := ParseEnv([]byte("vars:secret [\n  password\n]\n"))
	e.SetVars(map[string]string{"A": "1"})
	want := "vars {\n  A: 1\n}\n\nvars:secret [\n  password\n]\n"
	if got := string(e.Bytes()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	empty := ParseEnv(nil)
	empty.SetVars(map[string]string{"A": "1"})
	if got := string(empty.Bytes()); got != "vars {\n  A: 1\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSecretsSingleLine(t *testing.T) {
	e := ParseEnv([]byte("vars:secret [a, b]\n"))
	if got := e.Secrets(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Secrets() = %v", got)
	}
}

func TestListEnvironments(t *testing.T) {
	coll := t.TempDir()
	dir := filepath.Join(coll, "environments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"prod.bru", "dev.bru", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	envs, err := ListEnvironments(coll)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range envs {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"dev", "prod"}) {
		t.Fatalf("names = %v", names)
	}
	if envs[0].Path != EnvironmentPath(coll, "dev") {
		t.Fatalf("path = %s", envs[0].Path)
	}
}

func TestValidEnvironmentName(t *testing.T) {
	for name, want := range map[string]bool{
		"dev": true, "local-dev": true, "Staging 2": true, "": false, "../x": false, "a/b": false, " x": false,
	} {
		if ValidEnvironmentName(name) != want {
			t.Errorf("ValidEnvironmentName(%q) != %v", name, want)
		}
	}
}
