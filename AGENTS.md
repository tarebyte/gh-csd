# AGENTS.md

gh csd is a `gh` extension that pairs a local git worktree with its own GitHub Codespace. It is written in Ruby and compiled to a native macOS binary with [Spinel](https://github.com/matz/spinel). The README covers usage; this file covers working on the code.

## Layout

- `bin/gh-csd.rb`: argument parsing and dispatch only. Keep logic in `lib/`.
- `lib/csd/commands.rb`: the `up`, `run`, `down`, `init`, and hidden `__watch` commands.
- `lib/csd/settings.rb`, `config.rb`: settings from env, the config file, and the origin remote.
- `lib/csd/worktree.rb`, `sync.rb`, `change_set.rb`: the local side and the rsync of changes.
- `lib/csd/codespace.rb`, `remote.rb`: `gh codespace` calls, and ssh/rsync to the codespace.
- `lib/csd/runner.rb`: every external command goes through `Runner`.

## Commands

- `make check`: run before finishing any change. It runs the tests, `spinel diff`, the type check, and the build.
- `make test`: the unit tests on CRuby only.
- `make doctor`: Spinel's health report. Run it when the binary behaves differently from CRuby. Findings inside Spinel's bundled packages are expected.
- Spinel must be at `~/src/matz/spinel/spinel`, or pass `SPINEL=<path>`. CI pins the version in `.github/workflows/ci.yml`.

## Spinel rules

- Give every method an inline RBS signature comment (`#: (String) -> bool`). `make types` fails on any untyped parameter or return.
- Spinel cannot type top-level methods, so `bin/gh-csd.rb` must not define helpers. Put them in a `lib/` class.
- Minitest runs only on CRuby. `spinel/smoke.rb` exercises the library under Spinel, and `spinel diff` compares its output with CRuby's. Update it whenever a constructor or method signature changes.

## Code style

- Match the surrounding code: small classes, short methods, and a one-line comment only for a why the code cannot show.
- Keep the code repo-neutral. No defaults, comments, or text that name a specific repo. Per-repo values belong in the user's `~/.config/gh-csd/config.yml`.

## Testing

- Add or update tests in `test/` for every change.
- Tests never run real `gh`, ssh, or rsync. Use `FakeRunner` (`test/support/fake_runner.rb`) and script its replies. Git runs for real in a throwaway repo (`test/support/git_repo.rb`).
- `csd_settings` in `test/test_helper.rb` builds settings for the fake origin `me/my-app`; tests never read the real config file.

## Security

- Escape every value placed into a remote shell command with `Shellwords`, as `remote.rb` and `sync.rb` do.
- Never pass the local `gh auth token` or other credentials into the codespace.
- Only the `release` job in CI has write access, and it runs only on `v*` tags. Keep it that way.

## Names other code depends on

Do not rename these without updating the readers:

- `cs-dev-codespace` and `cs-dev-status` in the worktree's git dir: read by a status line in the user's dotfiles.
- `.git/cs-dev-owner` in the codespace: marks which worktree owns it.
- `~/.local/state/gh-csd/setup-ready` and `setup-complete` in the codespace: written by the user's dotfiles.

## Commits and releases

- Commit subjects are short, imperative, and have no prefix or final period (for example, "Wait for services when up reuses a codespace").
- Rulesets block force pushes and deletion on `main`, and require the CI `check` job for everyone except admins.
- To release, push a new `v*` tag. Tags cannot be moved or deleted, so fix a bad release with a new version.
- `gh skill install` installs the skill from the latest tag, so skill changes reach users only with a release.
