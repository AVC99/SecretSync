package main

import (
	"reflect"
	"testing"

	"secretsync/internal/dotenv"
)

const template = `# API
API_KEY=
API_URL=https://api.example.com
db_password=
LOG_LEVEL=info
`

func TestFillFromTemplateExistingEnv(t *testing.T) {
	tpl := dotenv.Parse([]byte(template))
	env := dotenv.Parse([]byte("API_URL=http://localhost:3000\nLOCAL_ONLY=1\n"))
	secrets := map[string]string{
		"API_KEY":     "k",
		"DB_PASSWORD": "p", // matches db_password case-insensitively
		"EXTRA":       "x",
	}

	res := fillFromTemplate(tpl, env, secrets)

	wantVars := map[string]string{
		"API_KEY":     "k",
		"db_password": "p",
		"LOG_LEVEL":   "info", // missing from .env: template default
		// API_URL keeps the current .env value, so it is not touched.
	}
	if !reflect.DeepEqual(res.vars, wantVars) {
		t.Errorf("vars = %v, want %v", res.vars, wantVars)
	}
	if !reflect.DeepEqual(res.unused, []string{"EXTRA"}) {
		t.Errorf("unused = %v", res.unused)
	}
	if len(res.missing) != 0 {
		t.Errorf("missing = %v", res.missing)
	}

	env.MergeOrdered(res.vars, tpl.Keys(), "hdr")
	want := "API_URL=http://localhost:3000\nLOCAL_ONLY=1\n\n# hdr\nAPI_KEY=k\ndb_password=p\nLOG_LEVEL=info\n"
	if got := string(env.Bytes()); got != want {
		t.Errorf("merged:\n%s\nwant:\n%s", got, want)
	}
}

func TestFillFromTemplateNewEnv(t *testing.T) {
	tpl := dotenv.Parse([]byte(template))
	env := dotenv.Parse([]byte(template)) // new .env starts as a copy

	res := fillFromTemplate(tpl, env, map[string]string{"API_KEY": "k"})
	if !reflect.DeepEqual(res.vars, map[string]string{"API_KEY": "k"}) {
		t.Errorf("vars = %v", res.vars)
	}
	if !reflect.DeepEqual(res.missing, []string{"db_password"}) {
		t.Errorf("missing = %v", res.missing)
	}

	env.Merge(res.vars, "hdr")
	want := "# API\nAPI_KEY=k\nAPI_URL=https://api.example.com\ndb_password=\nLOG_LEVEL=info\n"
	if got := string(env.Bytes()); got != want {
		t.Errorf("merged:\n%s\nwant:\n%s", got, want)
	}
}
