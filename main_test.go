package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/AVC99/secretsync/internal/bruno"
)

// fakeSM serves secret values by name.
type fakeSM map[string]string

func (f fakeSM) ListSecrets(context.Context, *secretsmanager.ListSecretsInput, ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	return &secretsmanager.ListSecretsOutput{}, nil
}

func (f fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	v, ok := f[*in.SecretId]
	if !ok {
		return nil, errors.New("not found")
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(v)}, nil
}

func resetWarnings(t *testing.T) {
	t.Helper()
	warnings = nil
	t.Cleanup(func() { warnings = nil })
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCollect(t *testing.T) {
	resetWarnings(t)
	api := fakeSM{
		"app/dev":   `{"API_KEY":"one","PORT":8080,"bad key":"x"}`,
		"app/other": `{"API_KEY":"two"}`,
		"app/token": "plain-text",
	}

	vars, err := collect(context.Background(), api, []string{"app/dev", "app/other", "app/token"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"API_KEY": "two", "PORT": "8080", "APP_TOKEN": "plain-text"}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("vars = %v, want %v", vars, want)
	}
	if len(warnings) != 2 {
		t.Errorf("want warnings for the invalid key and the duplicate, got %q", warnings)
	}

	if _, err := collect(context.Background(), api, []string{"missing"}); err == nil {
		t.Error("want error for a missing secret")
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{
		"":          "(vacío)",
		"abcd":      "****",
		"abcde":     "ab****",
		"ñandú1234": "ña****",
	} {
		if got := mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRel(t *testing.T) {
	base := filepath.Join("a", "b")
	if got := rel(base, filepath.Join("a", "b", "c", ".env")); got != filepath.Join("c", ".env") {
		t.Errorf("rel inside = %q", got)
	}
	outside := filepath.Join("a", "x", ".env")
	if got := rel(base, outside); got != outside {
		t.Errorf("rel outside = %q, want the path unchanged", got)
	}
}

func newInjection(cwd string, vars map[string]string) *injection {
	return &injection{cwd: cwd, profile: "dev", region: "eu-west-1", names: []string{"app/dev"}, vars: vars, yes: true}
}

func TestBrunoApply(t *testing.T) {
	resetWarnings(t)
	dir := t.TempDir()
	collection := filepath.Join(dir, "bruno")
	write(t, filepath.Join(collection, "bruno.json"), `{"version":"1","name":"x","type":"collection"}`)
	envPath := bruno.EnvironmentPath(collection, "dev")
	write(t, envPath, "vars {\n  baseUrl: http://localhost\n  API_KEY: old\n}\nvars:secret [\n  TOKEN\n]\n")
	write(t, filepath.Join(collection, ".env"), "# local\nOTHER=1\n")

	tg := &brunoTarget{collection: collection, env: bruno.Environment{Name: "dev", Path: envPath},
		dotenvPath: filepath.Join(collection, ".env")}
	if err := tg.apply(newInjection(dir, map[string]string{"API_KEY": "k1", "DB_PASS": "p w", "TOKEN": "t"})); err != nil {
		t.Fatal(err)
	}

	bru := read(t, envPath)
	for _, want := range []string{"baseUrl: http://localhost", "API_KEY: {{process.env.API_KEY}}",
		"DB_PASS: {{process.env.DB_PASS}}", "vars:secret [\n  TOKEN\n]"} {
		if !strings.Contains(bru, want) {
			t.Errorf(".bru missing %q:\n%s", want, bru)
		}
	}
	if strings.Contains(bru, "TOKEN: ") {
		t.Errorf("a vars:secret key must not be added to vars:\n%s", bru)
	}

	env := read(t, filepath.Join(collection, ".env"))
	for _, want := range []string{"# local\nOTHER=1\n", "API_KEY=k1\n", "DB_PASS='p w'\n", "TOKEN=t\n"} {
		if !strings.Contains(env, want) {
			t.Errorf(".env missing %q:\n%s", want, env)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "TOKEN") {
		t.Errorf("want one warning about TOKEN, got %q", warnings)
	}
}

func TestBrunoApplyCreatesEnvironment(t *testing.T) {
	resetWarnings(t)
	dir := t.TempDir()
	envPath := bruno.EnvironmentPath(dir, "local")
	tg := &brunoTarget{collection: dir, env: bruno.Environment{Name: "local", Path: envPath},
		dotenvPath: filepath.Join(dir, ".env")}
	if err := tg.apply(newInjection(dir, map[string]string{"A": "1"})); err != nil {
		t.Fatal(err)
	}
	if got := read(t, envPath); !strings.Contains(got, "vars {\n  A: {{process.env.A}}\n}") {
		t.Errorf("new environment:\n%s", got)
	}
	if got := read(t, filepath.Join(dir, ".env")); !strings.Contains(got, "A=1\n") {
		t.Errorf(".env:\n%s", got)
	}
}

func TestEnvApplyFromTemplate(t *testing.T) {
	resetWarnings(t)
	dir := t.TempDir()
	tpl := filepath.Join(dir, ".env.template")
	write(t, tpl, "# config\nAPI_KEY=\nDB_PASSWORD=changeme\nLOG_LEVEL=info\n")

	tg := &envTarget{template: tpl, envPath: filepath.Join(dir, ".env")}
	if err := tg.apply(newInjection(dir, map[string]string{"api_key": "abc123", "UNUSED": "x"})); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, tg.envPath), "# config\nAPI_KEY=abc123\nDB_PASSWORD=changeme\nLOG_LEVEL=info\n"; got != want {
		t.Errorf(".env =\n%s\nwant\n%s", got, want)
	}

	// Second run on the existing .env keeps local edits and extra lines.
	write(t, tg.envPath, "# config\nAPI_KEY=abc123\nDB_PASSWORD=local\nLOG_LEVEL=info\nMINE=1\n")
	if err := tg.apply(newInjection(dir, map[string]string{"API_KEY": "new"})); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, tg.envPath), "# config\nAPI_KEY=new\nDB_PASSWORD=local\nLOG_LEVEL=info\nMINE=1\n"; got != want {
		t.Errorf(".env after update =\n%s\nwant\n%s", got, want)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	resetWarnings(t)
	dir := t.TempDir()
	tpl := filepath.Join(dir, ".env.example")
	write(t, tpl, "API_KEY=\n")

	inj := newInjection(dir, map[string]string{"API_KEY": "k"})
	inj.dryRun = true
	tg := &envTarget{template: tpl, envPath: filepath.Join(dir, ".env")}
	if err := tg.apply(inj); err != nil {
		t.Fatal(err)
	}
	if exists(tg.envPath) {
		t.Error("dry run created .env")
	}

	bt := &brunoTarget{collection: dir, env: bruno.Environment{Name: "dev", Path: bruno.EnvironmentPath(dir, "dev")},
		dotenvPath: filepath.Join(dir, "bruno.env")}
	if err := bt.apply(inj); err != nil {
		t.Fatal(err)
	}
	if exists(bt.env.Path) || exists(bt.dotenvPath) {
		t.Error("dry run wrote Bruno files")
	}
}

func TestCheckGitignoreAddsEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	resetWarnings(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, filepath.Join(dir, ".gitignore"), "node_modules")
	envPath := filepath.Join(dir, ".env")
	write(t, envPath, "A=1\n")

	if err := checkGitignore(dir, envPath, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, ".gitignore")); got != "node_modules\n.env\n" {
		t.Errorf(".gitignore = %q", got)
	}

	// Already ignored: nothing else is appended.
	if err := checkGitignore(dir, envPath, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, ".gitignore")); got != "node_modules\n.env\n" {
		t.Errorf(".gitignore after second run = %q", got)
	}
}
