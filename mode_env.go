package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"secretsync/internal/dotenv"
	"secretsync/internal/fsutil"
	"secretsync/internal/ui"
)

// templateNames are the file names recognized as .env templates, in order of preference.
var templateNames = []string{".env.example", ".env.template", ".env.sample", ".env.dist", "env.example"}

// envTarget fills a .env with the keys listed in a template such as .env.example.
type envTarget struct {
	template string
	envPath  string
}

func prepareEnv(cwd string, o options) (*envTarget, error) {
	tpl := o.template
	if tpl == "" {
		var found []string
		for _, n := range templateNames {
			if p := filepath.Join(cwd, n); exists(p) {
				found = append(found, p)
			}
		}
		switch len(found) {
		case 0:
			return nil, fmt.Errorf("no hay plantilla (%s) en %s; indica una con --template",
				strings.Join(templateNames, ", "), cwd)
		case 1:
			tpl = found[0]
		default:
			choices := make([]ui.Choice, len(found))
			for i, p := range found {
				choices[i] = ui.Choice{Label: filepath.Base(p), Value: p}
			}
			var err error
			if tpl, err = ui.Select("Plantilla", "Hay varias plantillas; elige cuál usar para rellenar .env.", choices, found[0]); err != nil {
				return nil, err
			}
		}
	}
	tpl, err := filepath.Abs(tpl)
	if err != nil {
		return nil, err
	}
	if !exists(tpl) {
		return nil, fmt.Errorf("no existe la plantilla %s", tpl)
	}

	envPath := o.envFile
	if envPath == "" {
		envPath = filepath.Join(filepath.Dir(tpl), ".env")
	}
	if envPath, err = filepath.Abs(envPath); err != nil {
		return nil, err
	}
	if envPath == tpl {
		return nil, errors.New("la plantilla y el .env destino son el mismo fichero")
	}
	return &envTarget{template: tpl, envPath: envPath}, nil
}

func (t *envTarget) describe(cwd string) string {
	return fmt.Sprintf("%s (plantilla %s)", rel(cwd, t.envPath), filepath.Base(t.template))
}

// fillResult is the outcome of matching the secrets against a template.
type fillResult struct {
	vars    map[string]string // values to merge into the .env
	missing []string          // template keys that end up without a value
	unused  []string          // secret keys not present in the template
}

// fillFromTemplate decides the value of each template key: the secret value
// if there is one (keys match case-insensitively), otherwise the current
// .env value, otherwise the template's default.
func fillFromTemplate(tpl, env *dotenv.File, secretVars map[string]string) fillResult {
	byFold := map[string]string{}
	for k := range secretVars {
		byFold[strings.ToLower(k)] = k
	}

	res := fillResult{vars: map[string]string{}}
	used := map[string]bool{}
	for _, k := range tpl.Keys() {
		value, fromSecret := secretVars[k]
		if !fromSecret {
			if sk, ok := byFold[strings.ToLower(k)]; ok {
				value, fromSecret = secretVars[sk], true
				used[sk] = true
			}
		} else {
			used[k] = true
		}
		switch {
		case fromSecret:
			res.vars[k] = value
		default:
			if current, ok := env.Lookup(k); ok {
				value = current
			} else {
				value, _ = tpl.Lookup(k)
				res.vars[k] = value
			}
		}
		if value == "" {
			res.missing = append(res.missing, k)
		}
	}
	for k := range secretVars {
		if !used[k] {
			res.unused = append(res.unused, k)
		}
	}
	sort.Strings(res.unused)
	return res
}

func (t *envTarget) apply(inj *injection) error {
	tplData, err := os.ReadFile(t.template)
	if err != nil {
		return err
	}
	tpl := dotenv.Parse(tplData)
	if len(tpl.Keys()) == 0 {
		return fmt.Errorf("la plantilla %s no define ninguna variable", rel(inj.cwd, t.template))
	}

	created := !exists(t.envPath)
	env := dotenv.Parse(tplData) // a new .env starts as a copy of the template
	if !created {
		if env, err = dotenv.Load(t.envPath); err != nil {
			return err
		}
	}

	res := fillFromTemplate(tpl, env, inj.vars)
	changes := env.Diff(res.vars)
	label := rel(inj.cwd, t.envPath)
	if created {
		// Everything in a new file is new: compare the final values against nothing.
		final := map[string]string{}
		for _, k := range tpl.Keys() {
			final[k], _ = env.Lookup(k)
			if v, ok := res.vars[k]; ok {
				final[k] = v
			}
		}
		changes = dotenv.Diff(func(string) (string, bool) { return "", false }, final)
		res.vars = final
		label += " (nuevo, a partir de " + filepath.Base(t.template) + ")"
	}
	var notes []string
	if len(res.unused) > 0 {
		notes = append(notes, "no están en la plantilla (se ignoran): "+strings.Join(res.unused, ", "))
	}
	if len(res.missing) > 0 {
		warn("siguen sin valor: %s", strings.Join(res.missing, ", "))
	}

	write, err := inj.review([]ui.Section{section(label, changes, res.vars, true, notes...)})
	if err != nil || (!write && inj.dryRun) {
		return err
	}
	if !write {
		return checkGitignore(inj.cwd, t.envPath, inj.yes)
	}

	env.MergeOrdered(res.vars, tpl.Keys(), inj.header())
	if err := fsutil.WriteFile(t.envPath, env.Bytes(), 0o600); err != nil {
		return err
	}
	say(fmt.Sprintf("%s %s en %s", okStyle.Render("✔"), countChanges(changes), rel(inj.cwd, t.envPath)))

	return checkGitignore(inj.cwd, t.envPath, inj.yes)
}
