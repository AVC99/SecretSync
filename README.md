# secretsync

[![CI](https://github.com/AVC99/secretsync/actions/workflows/ci.yml/badge.svg?event=pull_request)](https://github.com/AVC99/secretsync/actions/workflows/ci.yml)
[![Release](https://github.com/AVC99/secretsync/actions/workflows/release.yml/badge.svg)](https://github.com/AVC99/secretsync/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/AVC99/secretsync?sort=semver)](https://github.com/AVC99/secretsync/releases/latest)
[![Homebrew](https://img.shields.io/badge/homebrew-AVC99%2Ftap%2Fsecretsync-FBB040?logo=homebrew&logoColor=white)](https://github.com/AVC99/homebrew-tap)
[![Go version](https://img.shields.io/github/go-mod/go-version/AVC99/secretsync)](go.mod)

Cross-platform CLI (macOS, Linux, Windows) that brings **AWS Secrets Manager** secrets into your project, in two modes:

- **Bruno**: declares the variables in a collection environment (`bruno/environments/<env>.bru`) as `KEY: {{process.env.KEY}}` and stores the real values in `bruno/.env`.
- **.env**: fills the project's `.env` from its template (`.env.example`, `.env.template`…).

```
  secretsync  AWS Secrets Manager secrets → Bruno / .env
  Mode Bruno  ›  Target environment "dev" (bruno)  ›  Profile dev  ›  Region eu-west-1

  ╭ Secrets  2/12 · 1 marked ─────────────────────────────────────────╮
  │  Filter myapp                                                      │
  │   ● myapp/dev  Bruno dev                                           │
  │   ○ myapp/token                                                    │
  ╰────────────────────────────────────────────────────────────────────╯
  ↑↓ move   space mark   ctrl+a all   enter continue   esc cancel
```

## Installation

**macOS / Linux with Homebrew**

```sh
brew install AVC99/tap/secretsync
brew upgrade secretsync   # update
```

**macOS / Linux without Homebrew**: downloads the latest release from GitHub, verifies its checksum and puts it in `/usr/local/bin` (or `~/.local/bin` if that is not writable). Run it again to update.

```sh
curl -fsSL https://raw.githubusercontent.com/AVC99/secretsync/main/install.sh | sh
# a specific version or directory:
curl -fsSL https://raw.githubusercontent.com/AVC99/secretsync/main/install.sh | VERSION=v0.1.0 INSTALL_DIR=~/bin sh
```

**With Go (any platform, including Windows)**: requires Go 1.26+.

```sh
go install github.com/AVC99/secretsync@latest
```

The binary ends up in `$(go env GOPATH)/bin`. If that is not in your PATH on Windows, add it from PowerShell (then open a new terminal):

```powershell
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';' + (go env GOPATH) + '\bin', 'User')
```

**Manual download**: [Releases](https://github.com/AVC99/secretsync/releases) has a `.tar.gz` (macOS/Linux) or `.zip` (Windows) per platform; extract it and copy `secretsync` to a directory in your PATH. The binaries are static (`CGO_ENABLED=0`).

The UI (tview/tcell) works in Terminal/iTerm, Linux terminals and Windows Terminal/PowerShell.

## Usage

From the root of your repo:

```sh
secretsync            # interactive menu
secretsync --dry-run  # same, but only shows what would change
```

The interactive flow:

1. **What do you want to do?** → Bruno or `.env`.
2. **Target**
   - Bruno: looks for `bruno/bruno.json` (also from subdirectories) and lets you pick an environment or create a new one.
   - `.env`: uses `.env.example` / `.env.template` / `.env.sample` / `.env.dist` in the current directory (asks if there are several).
3. **AWS profile**, read from `~/.aws/credentials` and `~/.aws/config` (honours `AWS_SHARED_CREDENTIALS_FILE` / `AWS_CONFIG_FILE`).
4. **Region**: `--region` → the profile's → `AWS_REGION` / `AWS_DEFAULT_REGION` → picker.
5. **Secrets**: the account's secrets with a filter; mark one or more.
6. **Review**: per-file diff (values masked) and a *Write changes* button.
7. If the `.env` is not ignored by git, it offers to add it to `.gitignore`.

When it exits, a summary of what was done stays printed in the terminal.

### Keys

| Key | Action |
|---|---|
| `↑` `↓` | move |
| typing | filter the list |
| `space` | mark / unmark a secret |
| `ctrl+a` | mark / unmark all visible |
| `enter` | select / continue |
| `←` `→` / `tab` | switch button |
| `esc` | clear the filter; if empty, cancel |
| `ctrl+c` | cancel at any time |

### How secrets are converted

- JSON secret `{"KEY":"value"}` → one variable per key. Numbers and booleans keep their literal; nested objects/arrays become compact JSON.
- Plain-text secret → one variable named after the normalised secret name (`my-app/db_pass` → `MY_APP_DB_PASS`).
- If a key appears in several secrets, the last one selected wins (with a warning).

### Bruno mode

- The environment gets `KEY: {{process.env.KEY}}` inside `vars { }` (the block or the file are created if missing). The rest of the file is left untouched.
- The real value goes to `bruno/.env`, which is the only `.env` Bruno reads and must not be committed.
- Variables in `vars:secret [ ]` cannot be filled from outside (Bruno encrypts them locally); if a secret matches one of them you get a warning and the `.bru` is not changed for it.
- If Bruno is open, reload the collection to see the changes.

#### Where do the values come from?

Everything stays on your machine:

- The `.bru` file only stores references (`API_KEY: {{process.env.API_KEY}}`), so it is safe to commit and share.
- The values live in `bruno/.env` on each machine (`0600`, git-ignored). Every teammate runs `secretsync` to get their own; secrets never go through the repo.
- Bruno reads `bruno/.env` when the collection is opened or the file changes. A variable that is not in it is taken from the environment of the Bruno process itself; if both define it, `.env` wins.
- Bruno does not call AWS when sending requests: after a secret is rotated in Secrets Manager, run `secretsync` again.
- On macOS, Bruno launched from the Dock or Spotlight does not see variables exported in `~/.zshrc`, so keep the values in `bruno/.env`.
- In CI with `bru run`, generate the file first (`secretsync --mode bruno --environment ci --profile … --secret … --yes`) or export the variables in the job.

### .env mode

- Only keys present in the template are filled (names are compared case-insensitively). Secret keys that are not in the template are ignored and listed.
- If `.env` does not exist it is created as a copy of the template (comments included) with the values filled in.
- If it exists, keys coming from the secrets are updated, missing template keys are added with the template's default and every other line is kept.
- Keys that are left without a value are reported.

In both modes writes are atomic and a new `.env` is created with `0600` permissions.

## Flags

| Flag | Description |
|---|---|
| `--mode bruno\|env` | skip the menu (useful in scripts) |
| `--profile` | AWS profile |
| `--region` | AWS region |
| `--filter` | filter the secret list by name or description |
| `--secret` | secret to inject; repeatable. Skips the selection |
| `--environment` | [bruno] environment (created if missing) |
| `--dir` | [bruno] collection path |
| `--template` | [env] template |
| `--env-file` | [env] target `.env` (default: next to the template) |
| `--dry-run` | show the changes without writing |
| `--yes` | do not ask for confirmation (and add the `.env` to `.gitignore` if needed) |
| `--version` | print the version |

Without an interactive terminal (CI, scripts) it falls back to plain-text output; pass everything as flags:

```sh
secretsync --mode bruno --environment dev --profile dev --secret myapp/dev --yes
secretsync --mode env --profile dev --secret myapp/dev --secret myapp/token --yes
```

## Development

```sh
make test           # go vet + go test
make build          # ./secretsync
make install        # installs to $GOBIN / ~/go/bin and explains how to add it to the PATH on each platform
make dist           # binaries in dist/ for darwin|linux|windows × amd64|arm64
make snapshot       # full release in dist/ (archives, checksums, cask) without publishing; requires goreleaser
```

To test without real AWS you can point the SDK at a local endpoint with `AWS_ENDPOINT_URL_SECRETS_MANAGER=http://127.0.0.1:4566` (LocalStack, for example).

## Workflow and releases

Work happens on `dev` and lands in `main` through a pull request:

1. **PR to `main` or `dev`** (and every push to the PR branch) → `ci` workflow: `go vet` and `go test` on Linux, macOS and Windows, plus a snapshot `goreleaser` run to check the release can be built. GitHub Copilot reviews the PR automatically.
2. **Merge** → done manually by the repo owner once the checks pass.
3. **Merge into `main`** → `release` workflow: runs the tests again, computes the next version, creates the tag and runs GoReleaser, which publishes the Release (binaries and `checksums.txt`) and updates the cask in [AVC99/homebrew-tap](https://github.com/AVC99/homebrew-tap).

The version comes from the PR labels:

| Label | Example |
|---|---|
| (none) | `v0.1.0` → `v0.1.1` |
| `minor` | `v0.1.0` → `v0.2.0` |
| `major` | `v0.1.0` → `v1.0.0` |
| `no-release` | nothing is published |

The `release` workflow needs the `TAP_DEPLOY_KEY` secret in this repo: the private key of a deploy key with write access to `homebrew-tap`.
