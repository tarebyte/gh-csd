# gh csd

Edit a GitHub repo in a local worktree, and run it in that worktree's own Codespace.

- `gh csd up <branch>` makes a worktree next to your main checkout (`~/src/my-app` → `~/src/my-app-<branch>`) and a codespace for it.
- `gh csd run <command>` copies the worktree's changes to the codespace with rsync, then runs the command there over SSH.
- `gh csd down <branch>` deletes the codespace, the worktree, and the branch.

Each worktree has its own codespace, so you can work on several branches at once.

## Requirements

- A Mac with Apple silicon.
- `gh`, logged in with the `codespace` scope: `gh auth refresh -s codespace`.
- `rsync` and `ssh`.
- A local clone of the repo, with its `origin` remote on github.com.

## Get started

### 1. Install

```sh
gh extension install tarebyte/gh-csd
```

Upgrade later with `gh extension upgrade csd`.

### 2. Create a config file

```sh
gh csd init
```

`init` writes a commented template to your config file (`~/.config/gh-csd/config.yml` by default). It does not overwrite a file that is already there.

Open the file and set `machine`. `up` does not start without it. To list the machine types for a repo, run:

```sh
gh api repos/<owner/name>/codespaces/machines --jq '.machines[].name'
```

For the other settings, see [Configuration](#configuration).

### 3. Update your dotfiles

`up` waits for marker files that your [dotfiles](https://docs.github.com/en/codespaces/setting-your-user-preferences/personalizing-github-codespaces-for-your-account#dotfiles) write in the codespace. Without them, `up` waits 20 minutes on each new codespace before it continues.

| File | Effect |
| --- | --- |
| `~/.local/state/gh-csd/setup-ready` | `up` waits up to 20 minutes for this file, then continues without it. |
| `~/.local/state/gh-csd/setup-complete` | gh csd sends a "ready for use" notification when this file appears. |

Add this to your dotfiles install script:

```sh
if [ -n "${CODESPACES:-}" ]; then
  mkdir -p "$HOME/.local/state/gh-csd"
  # ...install the tools you need before your first `gh csd run`...
  touch "$HOME/.local/state/gh-csd/setup-ready"
  # ...slow installs that can finish later...
  touch "$HOME/.local/state/gh-csd/setup-complete"
fi
```

For a full example, see [`script/install-codespace-tools`](https://github.com/tarebyte/dotfiles/blob/main/script/install-codespace-tools) and [`script/setup`](https://github.com/tarebyte/dotfiles/blob/main/script/setup) in tarebyte/dotfiles.

### 4. Start, run, and tear down

Run these from inside your clone or one of its worktrees.

```sh
gh csd up my-feature              # worktree + codespace, waits until both are ready
cd ../my-app-my-feature
# ...edit files locally...
gh csd run bin/test test/user_test.rb
gh csd run                        # sync only
gh csd down my-feature            # refuses if the branch has unsaved work
gh csd down --force my-feature    # deletes it anyway
```

A new codespace takes several minutes. `up` prints the worktree and the codespace when both are ready.

If the devcontainer asks for extra repository permissions, the first `up` opens your browser to approve them. `up` continues after you approve.

`up` on an existing branch reuses its worktree and codespace.

## Use it from an agent

The [`codespace-dev`](skills/codespace-dev/SKILL.md) skill teaches your agent to use gh csd: start an environment, run tests in it, and tear it down. Install it with `gh skill`, which supports Claude Code, GitHub Copilot, Cursor, Codex, and many other agents:

```sh
gh skill install tarebyte/gh-csd codespace-dev --scope user
```

`gh skill` asks which agent to install it for. Update it later with `gh skill update`.

Then ask the agent in plain words. For example:

- "Spin up a codespace for `me/fix-login`."
- "Run `test/user_test.rb` in my codespace."
- "Sync my changes and run the linter in the codespace."
- "The test failed in the codespace. Fix it and run it again."
- "Tear down the codespace for `me/fix-login`."

To install it as a Claude Code or Copilot CLI plugin instead, see [docs/plugins.md](docs/plugins.md).

## Configuration

Set settings in a config file or in environment variables. An environment variable wins over the config file. `gh csd init` writes a template with every setting. `machine` has no default, so set it before your first `up`.

The config file is the first of:

1. `$GH_CSD_CONFIG`
2. `$XDG_CONFIG_HOME/gh-csd/config.yml`
3. `~/.config/gh-csd/config.yml`

| Config key | Environment variable | Default |
| --- | --- | --- |
| `machine` | `CS_MACHINE` | none; `up` requires it |
| `devcontainer` | `CS_DEVCONTAINER` | `.devcontainer/devcontainer.json` |
| `default_permissions` | `CS_DEFAULT_PERMISSIONS` | `false` |
| `dir` | `REMOTE_DIR` | `/workspaces/<repo name>` |
| `branch` | | origin's default branch, else `main` |
| `ready_check` | `CS_READY_CHECK` | none (no wait) |

Put keys at the top level, or under `repos: <owner/name>:` for one repo. A repo's section wins over the top level:

```yaml
machine: standardLinux32gb
repos:
  my-org/my-app:
    machine: premiumLinux
    ready_check: nc -z localhost 5432
```

`ready_check` is a command that `up` runs in the codespace every 5 seconds, for up to 5 minutes, until it succeeds. Use it when your services take a while to accept connections after the codespace starts. `none` skips it.

Environment only:

- `CS`: the codespace for `run`, instead of the worktree's own.
- `CS_TAKEOVER`: let `run` claim a codespace that another worktree owns.

## Development

You need [Spinel](https://github.com/matz/spinel) to build. The Makefile looks for it at `~/src/matz/spinel/spinel`; set `SPINEL` to change that.

```sh
make test    # unit tests on CRuby
make check   # tests, spinel diff, type check, and build
```

To release, push a `v*` tag. CI builds `gh-csd-darwin-arm64` and attaches it to a GitHub release.
