# AGENTS.md

gh csd is a `gh` extension that pairs a local git worktree with its own GitHub Codespace. It is written in Go on [go-gh](https://github.com/cli/go-gh), gh's own library for extensions. The README covers usage; this file covers working on the code.

## Layout

- `main.go`: the signal context and `cmd.Execute`, nothing else.
- `cmd/`: one file per cobra command, as gh lays out `pkg/cmd/codespace`. Each has a `newXCmd()` constructor (no package globals or `init()`), parses flags, and calls `internal/csd`. `root.go` holds `Execute`, the exit codes (0 done, 1 failed, 2 cancelled, as gh), and flag errors; `wire.go` builds the real runner, REST client, terminal, prompter, and browser.
- `internal/csd/commands.go`: the `up`, `run`, `status`, `down`, and `init` commands.
- `internal/csd/settings.go`, `config.go`: settings from flags, the config file, and the origin remote.
- `internal/csd/worktree.go`, `sync.go`, `changeset.go`: the local side and the rsync of changes.
- `internal/csd/codespace.go`, `remote.go`: codespaces through the REST API (`API`), `gh codespace create` and `ssh --config` through the gh that runs the extension, waiting, and ssh/rsync to the codespace.
- `internal/csd/runner.go`: every external command goes through `Runner`, which returns an error only when a program cannot start.
- `script/build.sh`: the release build, macOS and Linux only.

## Commands

- `make check`: run before finishing any change. It checks `gofmt`, then runs `go vet`, the tests, and the build. CI runs the same steps on macOS and Linux, the platforms releases ship. Windows is left out: run needs rsync and ssh ControlMaster.
- `make test`: the tests only.

## Use go-gh

Reach for go-gh before writing your own: `pkg/api` for GitHub (a 404 is `*api.HTTPError`; never treat another failure as "not found"), `gh.Path` for the gh executable, `pkg/ssh` to resolve ssh host aliases, `pkg/jq` and `pkg/template` for `--jq` and `--template`, `pkg/jsonpretty` for JSON, `pkg/tableprinter` for tables, `pkg/prompter` for questions, `pkg/term` for TTY checks and width, and `pkg/browser` to open URLs. They behave as gh does.

## CLI design

Follow `gh`: flags named like `gh codespace` (`-m`, `-c`, `--devcontainer-path`), no setting that only an environment variable can set, a prompt only in a terminal with a flag for scripts, `--json <fields>` for output that scripts read, and progress in the foreground.

## Code style

- Pass `context.Context` first to anything that runs a program, calls the API, or waits; `main` cancels it on Ctrl-C.
- Never let a failed step look like an empty result: an error from git, gh, or the API stops the command before it changes anything.

- Match the surrounding code: small types, short functions, errors returned rather than logged, and a one-line comment only for a why the code cannot show.
- Keep the code repo-neutral. No defaults, comments, or text that name a specific repo. Per-repo values belong in the user's `~/.config/gh-csd/config.yml`.

## Testing

- Add or update tests for every change, next to the code (`*_test.go`).
- Tests never run real `gh`, ssh, rsync, or the GitHub API. Use `fakeRunner` (`internal/csd/helpers_test.go`) and script its replies with `on` and `onSSH`; use `fakeAPI` for REST calls. Git runs for real in a throwaway repo (`gitRepo`), and `fakeClock` makes waits instant.
- `testSettings` builds settings for the fake origin `me/my-app`; tests never read the real config file.

## Security

- Escape every value placed into a remote shell command with `ShellEscape` or `ShellJoin`, as `remote.go` and `sync.go` do.
- Never pass the local `gh auth token` or other credentials into the codespace.
- Only the `release` job in CI has write access, and it runs only on `v*` tags. Keep it that way.

## Names other code depends on

Do not rename these without updating the readers:

- `cs-dev-codespace` and `cs-dev-status` in the worktree's git dir: read by a status line in the user's dotfiles. New readers should use `gh csd status --json` instead.
- `.git/cs-dev-owner` in the codespace: marks which worktree owns it.
- `~/.local/state/gh-csd/setup-ready` and `setup-complete` in the codespace: written by the user's dotfiles.

## Commits and releases

- Commit subjects are short, imperative, and have no prefix or final period (for example, "Wait for services when up reuses a codespace").
- Rulesets block force pushes and deletion on `main`, and require the CI `check` job for everyone except admins.
- To release, push a new `v*` tag. Tags cannot be moved or deleted, so fix a bad release with a new version.
- `gh skill install` installs the skill from the latest tag, so skill changes reach users only with a release.
