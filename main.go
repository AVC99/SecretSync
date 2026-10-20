// Command secretsync injects AWS Secrets Manager secrets into a Bruno
// collection or into a project's .env file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/charmbracelet/lipgloss"

	"github.com/AVC99/secretsync/internal/awsprofiles"
	"github.com/AVC99/secretsync/internal/dotenv"
	"github.com/AVC99/secretsync/internal/secrets"
	"github.com/AVC99/secretsync/internal/ui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// buildVersion returns version, falling back to the module version recorded
// by `go install …@vX.Y.Z`, which does not pass -ldflags.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// Styles for the plain-text output printed to the terminal (directly in
// non-interactive runs, or after the UI closes).
var (
	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	updatedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	titleStyle   = lipgloss.NewStyle().Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
)

const (
	modeBruno = "bruno"
	modeEnv   = "env"
)

// warnings collected during the run, also shown on the review screen.
var warnings []string

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

type options struct {
	mode, profile, region, filter string
	dir, environment              string // bruno mode
	template, envFile             string // env mode
	secrets                       stringList
	dryRun, yes, version          bool
}

// target is a destination for the secrets: a Bruno environment or a .env file.
type target interface {
	// describe returns a short description of the destination.
	describe(cwd string) string
	// apply reviews the pending changes and, if accepted, writes them.
	apply(inj *injection) error
}

// injection holds what was fetched from AWS and how to apply it.
type injection struct {
	cwd             string
	profile, region string
	names           []string
	vars            map[string]string
	dryRun, yes     bool
}

func (inj *injection) header() string {
	return fmt.Sprintf("--- secretsync %s · %s/%s · %s ---",
		time.Now().Format("2006-01-02"), inj.profile, inj.region, strings.Join(inj.names, ", "))
}

// review prints a summary of the changes and decides whether to write them:
// never in dry-run mode or when nothing changes, always with --yes, and
// otherwise as chosen by the user on the review screen.
func (inj *injection) review(sections []ui.Section) (bool, error) {
	changed := false
	for _, s := range sections {
		printSection(s)
		for _, c := range s.Changes {
			changed = changed || c.Sign != '='
		}
	}
	d := ui.ReviewData{Sections: sections, Warnings: warnings}

	var status string
	switch {
	case inj.dryRun:
		say(dimStyle.Render("--dry-run: nothing was written."))
		status = "Dry run (--dry-run): nothing will be written"
	case !changed:
		say(okStyle.Render("✔") + " Everything is up to date.")
		status = "Everything is up to date"
	case inj.yes:
		return true, nil
	default:
		d.CanWrite = true
		ok, err := ui.Review(d)
		if err == nil && !ok {
			err = ui.ErrAborted
		}
		return ok, err
	}
	if ui.Active() {
		d.Status = status
		_, err := ui.Review(d)
		return false, err
	}
	return false, nil
}

func main() {
	err := run()
	ui.Close()
	if err != nil {
		if errors.Is(err, ui.ErrAborted) || errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, warnStyle.Render("error:"), err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.mode, "mode", "", "skip the menu: bruno | env (for scripts)")
	flag.StringVar(&o.profile, "profile", "", "AWS profile (asked if omitted)")
	flag.StringVar(&o.region, "region", "", "AWS region (defaults to the profile's)")
	flag.StringVar(&o.filter, "filter", "", "filter the secret list by name or description")
	flag.Var(&o.secrets, "secret", "secret to inject; repeatable (skips the interactive selection)")
	flag.StringVar(&o.dir, "dir", "", "[bruno] collection path (default ./bruno, searching upwards)")
	flag.StringVar(&o.environment, "environment", "", "[bruno] Bruno environment (created if missing)")
	flag.StringVar(&o.template, "template", "", "[env] template (default .env.example, .env.template… in the current directory)")
	flag.StringVar(&o.envFile, "env-file", "", "[env] target .env (default .env next to the template)")
	flag.BoolVar(&o.dryRun, "dry-run", false, "show the changes without writing anything")
	flag.BoolVar(&o.yes, "yes", false, "do not ask for confirmation")
	flag.BoolVar(&o.version, "version", false, "print the version")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `secretsync — inject AWS Secrets Manager secrets into Bruno or a .env file

Usage:
  secretsync [flags]

On start it asks what you want to do:
  Bruno  pick an environment in bruno/environments/; each key is declared
         as KEY: {{process.env.KEY}} and its value goes to bruno/.env.
  .env   fill .env with the keys of .env.example (or .env.template…)
         using the secret values.

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()
	return o
}

func run() error {
	o := parseFlags()
	if o.version {
		fmt.Println("secretsync", buildVersion())
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ui.OnInterrupt(cancel) // Ctrl+C inside the UI is a key press, not a signal

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	mode, err := chooseMode(o.mode)
	if err != nil {
		return err
	}
	var t target
	switch mode {
	case modeBruno:
		ui.SetStep("Mode", "Bruno")
		t, err = prepareBruno(cwd, o)
	case modeEnv:
		ui.SetStep("Mode", ".env")
		t, err = prepareEnv(cwd, o)
	}
	if err != nil {
		return err
	}
	ui.SetStep("Target", t.describe(cwd))

	profile, err := chooseProfile(o.profile)
	if err != nil {
		return err
	}
	ui.SetStep("Profile", profile)
	region, err := chooseRegion(o.region, profile)
	if err != nil {
		return err
	}
	ui.SetStep("Region", region)
	say(fmt.Sprintf("%s profile %s · region %s · target %s",
		dimStyle.Render("›"), profile, region, t.describe(cwd)))

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("loading AWS config for profile %q: %w", profile, err)
	}
	client := secretsmanager.NewFromConfig(cfg)

	names := []string(o.secrets)
	if len(names) == 0 {
		ui.Loading(fmt.Sprintf("Listing secrets (profile %s, region %s)…", profile, region))
		list, err := secrets.List(ctx, client, o.filter)
		if err != nil {
			return fmt.Errorf("listing secrets (profile %s, region %s): %w", profile, region, err)
		}
		if len(list) == 0 {
			if o.filter != "" {
				return fmt.Errorf("no secret matches %q in %s", o.filter, region)
			}
			return fmt.Errorf("no secrets in the account of profile %s in %s", profile, region)
		}
		choices := make([]ui.Choice, len(list))
		for i, s := range list {
			choices[i] = ui.Choice{Label: s.Name, Value: s.Name, Hint: s.Description}
		}
		names, err = ui.MultiSelect("Secrets", "Mark the secrets to inject. Each JSON key becomes a variable.", choices)
		if err != nil {
			return err
		}
	}
	secretsStep := names[0]
	if len(names) > 1 {
		secretsStep += fmt.Sprintf(" +%d", len(names)-1)
	}
	ui.SetStep("Secrets", secretsStep)

	ui.Loading(fmt.Sprintf("Reading %d secret(s)…", len(names)))
	vars, err := collect(ctx, client, names)
	if err != nil {
		return err
	}
	if len(vars) == 0 {
		return errors.New("the selected secrets contain no usable keys")
	}

	return t.apply(&injection{
		cwd: cwd, profile: profile, region: region,
		names: names, vars: vars,
		dryRun: o.dryRun, yes: o.yes,
	})
}

func chooseMode(flagValue string) (string, error) {
	switch flagValue {
	case modeBruno, modeEnv:
		return flagValue, nil
	case "":
	default:
		return "", fmt.Errorf("--mode must be %q or %q", modeBruno, modeEnv)
	}
	return ui.Select("What do you want to do?", "", []ui.Choice{
		{Label: "Inject secrets into a Bruno environment", Value: modeBruno,
			Hint: "KEY: {{process.env.KEY}} in the .bru + value in bruno/.env"},
		{Label: "Fill .env from its template", Value: modeEnv,
			Hint: ".env.example → .env"},
	}, modeBruno)
}

func chooseProfile(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	profiles, err := awsprofiles.List()
	if err != nil {
		return "", fmt.Errorf("reading AWS profiles: %w", err)
	}
	switch len(profiles) {
	case 0:
		return "", fmt.Errorf("no profiles in %s or %s", awsprofiles.CredentialsFile(), awsprofiles.ConfigFile())
	case 1:
		return profiles[0], nil
	}
	def := os.Getenv("AWS_PROFILE")
	if !slices.Contains(profiles, def) {
		def = profiles[0]
		if slices.Contains(profiles, "default") {
			def = "default"
		}
	}
	return ui.SelectProfile(profiles, def)
}

func chooseRegion(flagValue, profile string) (string, error) {
	for _, r := range []string{
		flagValue,
		awsprofiles.Region(profile),
		os.Getenv("AWS_REGION"),
		os.Getenv("AWS_DEFAULT_REGION"),
	} {
		if r != "" {
			return r, nil
		}
	}
	return ui.SelectRegion()
}

// collect fetches and flattens the secrets. When a key appears in several
// secrets the last one wins.
func collect(ctx context.Context, api secrets.API, names []string) (map[string]string, error) {
	vars := map[string]string{}
	source := map[string]string{}
	for _, name := range names {
		value, err := secrets.Get(ctx, api, name)
		if err != nil {
			return nil, fmt.Errorf("reading secret %q: %w", name, err)
		}
		flat := secrets.Flatten(name, value)
		keys := make([]string, 0, len(flat))
		for k := range flat {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !dotenv.ValidKey(k) {
				warn("ignoring key %q of %s: not a valid variable name", k, name)
				continue
			}
			if prev, ok := source[k]; ok && prev != name {
				warn("%s is in both %s and %s; using the value from %s", k, prev, name, name)
			}
			vars[k] = flat[k]
			source[k] = name
		}
	}
	return vars, nil
}

// section builds the review section of one file. Values are masked unless
// they are not secret (e.g. {{process.env.X}} references).
func section(title string, c dotenv.Changes, vars map[string]string, masked bool, notes ...string) ui.Section {
	s := ui.Section{Title: title, Notes: notes}
	show := func(k string) string {
		if masked {
			return mask(vars[k])
		}
		return vars[k]
	}
	for _, k := range c.Added {
		s.Changes = append(s.Changes, ui.Change{Sign: '+', Key: k, Value: show(k)})
	}
	for _, k := range c.Updated {
		s.Changes = append(s.Changes, ui.Change{Sign: '~', Key: k, Value: show(k)})
	}
	for _, k := range c.Unchanged {
		s.Changes = append(s.Changes, ui.Change{Sign: '=', Key: k})
	}
	return s
}

func printSection(s ui.Section) {
	say(titleStyle.Render(s.Title))
	width := 0
	for _, c := range s.Changes {
		width = max(width, len(c.Key))
	}
	for _, c := range s.Changes {
		style, value := addedStyle, c.Value
		switch c.Sign {
		case '~':
			style = updatedStyle
		case '=':
			style, value = dimStyle, "unchanged"
		}
		say(fmt.Sprintf("  %s %-*s %s", style.Render(string(c.Sign)), width, c.Key, dimStyle.Render(value)))
	}
	for _, n := range s.Notes {
		say(dimStyle.Render("  " + n))
	}
}

func countChanges(c dotenv.Changes) string {
	return fmt.Sprintf("%d added, %d updated", len(c.Added), len(c.Updated))
}

// checkGitignore warns when a secrets file could end up committed and
// offers to add it to the .gitignore next to it. Silently does nothing
// outside a git repository or when git is not installed.
func checkGitignore(cwd, path string, assumeYes bool) error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	dir, base := filepath.Dir(path), filepath.Base(path)

	if exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", base).Run() == nil {
		warn("%s is tracked by git. Remove it from the repo with: git rm --cached %s", base, path)
		return nil
	}
	err := exec.Command("git", "-C", dir, "check-ignore", "-q", base).Run()
	var exitErr *exec.ExitError
	if err == nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		// Ignored, not a repository, or git failed.
		return nil
	}

	gitignore := filepath.Join(dir, ".gitignore")
	warn("%s is not in .gitignore: you could commit secrets", base)
	add := assumeYes
	if !add {
		if !ui.Interactive() {
			return nil
		}
		question := fmt.Sprintf("%s is not ignored by git and contains secrets.\n\nAdd %s to %s?",
			rel(cwd, path), base, rel(cwd, gitignore))
		if add, err = ui.Confirm(".gitignore", question, true); err != nil {
			return err
		}
	}
	if !add {
		return nil
	}
	return appendLine(gitignore, base, rel(cwd, gitignore))
}

func appendLine(path, line, label string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		line = "\n" + line
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	say(okStyle.Render("✔") + " Added " + strings.TrimSpace(line) + " to " + label)
	return nil
}

func mask(v string) string {
	r := []rune(v)
	if len(r) == 0 {
		return "(empty)"
	}
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****"
}

func rel(base, path string) string {
	if r, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// say prints a line to stdout (deferred until the UI closes, if open).
func say(s string) { ui.Print(s) }

func warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	warnings = append(warnings, msg)
	ui.Warn(warnStyle.Render("⚠") + " " + msg)
}
