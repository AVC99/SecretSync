package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/AVC99/secretsync/internal/bruno"
	"github.com/AVC99/secretsync/internal/dotenv"
	"github.com/AVC99/secretsync/internal/fsutil"
	"github.com/AVC99/secretsync/internal/ui"
)

// brunoTarget declares each key in a Bruno environment as
// KEY: {{process.env.KEY}} and stores the value in the collection's .env,
// which is the only .env Bruno reads.
type brunoTarget struct {
	collection string
	env        bruno.Environment
	dotenvPath string
}

const newEnvironment = "\x00new"

func prepareBruno(cwd string, o options) (*brunoTarget, error) {
	collection, err := bruno.FindCollection(cwd, o.dir)
	if err != nil {
		return nil, err
	}
	env, err := chooseEnvironment(cwd, collection, o.environment)
	if err != nil {
		return nil, err
	}
	return &brunoTarget{
		collection: collection,
		env:        env,
		dotenvPath: filepath.Join(collection, ".env"),
	}, nil
}

func chooseEnvironment(cwd, collection, flagValue string) (bruno.Environment, error) {
	if flagValue != "" {
		if !bruno.ValidEnvironmentName(flagValue) {
			return bruno.Environment{}, fmt.Errorf("invalid environment name: %q", flagValue)
		}
		return bruno.Environment{Name: flagValue, Path: bruno.EnvironmentPath(collection, flagValue)}, nil
	}

	envs, err := bruno.ListEnvironments(collection)
	if err != nil {
		return bruno.Environment{}, err
	}
	choice := newEnvironment
	if len(envs) > 0 {
		choices := make([]ui.Choice, 0, len(envs)+1)
		for _, e := range envs {
			choices = append(choices, ui.Choice{Label: e.Name, Value: e.Name, Hint: rel(cwd, e.Path)})
		}
		choices = append(choices, ui.Choice{Label: "+ Create a new environment", Value: newEnvironment})
		choice, err = ui.Select("Bruno environment",
			"Environment where the variables are declared. The values go to "+rel(cwd, filepath.Join(collection, ".env"))+".",
			choices, envs[0].Name)
		if err != nil {
			return bruno.Environment{}, err
		}
	}
	if choice != newEnvironment {
		return bruno.Environment{Name: choice, Path: bruno.EnvironmentPath(collection, choice)}, nil
	}

	name, err := ui.Input("New environment",
		"Will create "+rel(cwd, bruno.EnvironmentPath(collection, "<name>")), "local",
		func(s string) error {
			switch {
			case !bruno.ValidEnvironmentName(s):
				return errors.New("use letters, digits, spaces, '.', '-' or '_'")
			case exists(bruno.EnvironmentPath(collection, s)):
				return errors.New("an environment with that name already exists")
			}
			return nil
		})
	if err != nil {
		return bruno.Environment{}, err
	}
	return bruno.Environment{Name: name, Path: bruno.EnvironmentPath(collection, name)}, nil
}

func (t *brunoTarget) describe(cwd string) string {
	return fmt.Sprintf("environment %q (%s)", t.env.Name, rel(cwd, t.collection))
}

func (t *brunoTarget) apply(inj *injection) error {
	envData, err := os.ReadFile(t.env.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	bru := bruno.ParseEnv(envData)
	dot, err := dotenv.Load(t.dotenvPath)
	if err != nil {
		return err
	}

	refs := map[string]string{}
	secretVars := bru.Secrets()
	for k := range inj.vars {
		if slices.Contains(secretVars, k) {
			warn("%s is already a Bruno secret variable in %s; remove it from vars:secret to use the AWS value",
				k, t.env.Name)
			continue
		}
		refs[k] = bruno.ProcessEnvRef(k)
	}

	envChanges := dotenv.Diff(bru.Lookup, refs)
	dotChanges := dot.Diff(inj.vars)
	envLabel := rel(inj.cwd, t.env.Path)
	if envData == nil {
		envLabel += " (new)"
	}
	write, err := inj.review([]ui.Section{
		section(envLabel, envChanges, refs, false),
		section(rel(inj.cwd, t.dotenvPath), dotChanges, inj.vars, true),
	})
	if err != nil || (!write && inj.dryRun) {
		return err
	}
	if !write {
		return checkGitignore(inj.cwd, t.dotenvPath, inj.yes)
	}

	dot.Merge(inj.vars, inj.header())
	if err := fsutil.WriteFile(t.dotenvPath, dot.Bytes(), 0o600); err != nil {
		return err
	}
	bru.SetVars(refs)
	if err := fsutil.WriteFile(t.env.Path, bru.Bytes(), 0o644); err != nil {
		return err
	}
	say(fmt.Sprintf("%s %s: %s · %s: %s", okStyle.Render("✔"),
		rel(inj.cwd, t.env.Path), countChanges(envChanges),
		rel(inj.cwd, t.dotenvPath), countChanges(dotChanges)))
	say(dimStyle.Render("  If Bruno is open, reload the collection to see the changes."))

	return checkGitignore(inj.cwd, t.dotenvPath, inj.yes)
}
