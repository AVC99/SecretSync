# CLAUDE.md

`secretsync` is a Go CLI (macOS, Linux, Windows) that reads AWS profiles, lists AWS Secrets Manager secrets and writes them into a Bruno environment (`KEY: {{process.env.KEY}}` in the `.bru` + value in `bruno/.env`) or into a project `.env` filled from its template (`.env.example`, `.env.template`…). See README.md for user-facing behaviour.

## Commands

```sh
make test       # go vet ./... && go test ./...
make build      # ./secretsync
make snapshot   # goreleaser release --snapshot --clean (needs goreleaser)
gofmt -l .      # must print nothing
```

Go 1.26+ (a dependency requires it). Module path: `github.com/AVC99/secretsync`.

## Layout

- `main.go`: flags, `run()` flow (mode → target → profile → region → secrets → `target.apply`), review/summary, `.gitignore` check.
- `mode_bruno.go` / `mode_env.go`: the two `target` implementations.
- `internal/ui`: tview TUI (`prompt.go`, `picker.go`). Falls back to plain text output when there is no TTY; then every choice must come from flags.
- `internal/dotenv`, `internal/bruno`: line-based parsers that preserve the original file (comments, order, unknown lines) and only touch the keys they change.
- `internal/secrets`, `internal/awsprofiles`, `internal/fsutil` (atomic writes).

## Conventions

- All user-facing text, docs, comments and commit messages are in English.
- `.env` parsing/quoting follows npm `dotenv` (what Bruno uses): single quotes and backticks are literal and may span lines, double quotes expand `\n`, unquoted values stop at `#`, last duplicate wins. `dotenv.Quote` must round-trip through `Parse`.
- Bruno `vars:secret` values are encrypted by Bruno and cannot be written from outside: warn and skip, never write them.
- Writes go through `fsutil.WriteFile` (temp + rename, keeps existing mode; new `.env` 0600, new `.bru` 0644).
- Never log or show secret values unmasked (`mask()` in `main.go`); `{{process.env.X}}` references are fine to show.

## Gotchas

- tview: `QueueUpdate`/`QueueUpdateDraw` block until the UI goroutine runs them. Never call them while holding `ui.mu` or from inside a UI callback (this caused a startup deadlock once).
- Output printed while the TUI is open is buffered and flushed on `ui.Close()`; use `say()`/`warn()`, not `fmt.Print`.
- `go install` takes the binary name from the module path's last element, so the repo and module must stay lowercase `secretsync`.
- Release archive names have no version (`secretsync_<os>_<arch>.tar.gz`) because `install.sh` downloads `releases/latest/download/<asset>`.

## Testing without AWS

- Unit tests use in-memory fakes of `secrets.API` (`main_test.go`, `internal/secrets/secrets_test.go`); `main_test.go` runs both modes end to end in temp dirs with `yes: true`.
- Manual runs: point the SDK at a local fake or LocalStack with `AWS_ENDPOINT_URL_SECRETS_MANAGER=http://127.0.0.1:4566` and use throwaway `AWS_SHARED_CREDENTIALS_FILE` / `AWS_CONFIG_FILE`.
- The TUI can be driven in tmux (`tmux new-session -d …`, `send-keys`, `capture-pane -p`). Send keys with small pauses: Esc followed quickly by an arrow arrives as Alt+arrow.

## Workflow

- Work on `dev`; open PRs `dev` → `main`. **Do not merge PRs or push to `main`**: the owner reviews and merges manually.
- CI (`.github/workflows/ci.yml`) runs on PRs: vet + tests on ubuntu/macos/windows and a goreleaser snapshot.
- Every push to `main` runs `release.yml`: version bump from the merged PR's labels (`major`, `minor`, default patch; `no-release` skips), tag, GoReleaser → GitHub Release + cask in `AVC99/homebrew-tap` (secret `TAP_DEPLOY_KEY`). Label docs/CI-only PRs `no-release`.
- After a PR is merged, fast-forward `dev` to `origin/main` before new work.
