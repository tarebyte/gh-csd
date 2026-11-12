---
name: codespace-dev
description: Use when starting or using a GitHub Codespace dev environment for a repo while editing locally — "kick off a codespace dev environment", "spin up a codespace for <branch>", "run this test in my codespace", "test in the codespace", "gh csd", "cs-dev", "tear down the codespace". Uses the gh csd extension, which pairs a local worktree with its own codespace, syncs the worktree's changes into it with rsync, and runs commands there over SSH.
---

# codespace-dev

Edit in a local worktree. Run tests in that worktree's own codespace, through
the `gh csd` extension. One worktree per codespace, so several sessions can
run in parallel.

`gh csd` comes from github.com/tarebyte/gh-csd. `gh csd --help` lists commands and settings.
If `gh csd` is missing, run `gh extension install tarebyte/gh-csd`.

gh csd works on the repo of the current checkout. It reads the repo name and the default branch from the `origin` remote, which must be on github.com.

## Start an environment

```sh
gh csd up            # the worktree you are in
gh csd up <branch>   # that branch's worktree, made if needed
```

- With no branch, `up` uses the current worktree. It fails in the main checkout.
- With a branch, it reuses the branch's existing worktree, wherever it is. Otherwise it creates `<main-checkout>-<branch>` (slashes become `-`), branched from the base branch (the `branch` setting, origin's default by default).
- Creates or reuses the worktree's codespace, then does a first sync. The codespace checks out the base branch, not yours; `run` copies your changes onto it.
- Prints `gh csd: ready  worktree=<path>  codespace=<name>`. Tell the user both values.
- A new codespace takes several minutes. Run `up` in the background and wait for it to finish.
- Without a terminal and with no `machine` set, `up` fails with `pass -m with one of: ...`. Ask the user which one, then pass `-m <machine>`.
- If the devcontainer asks for extra repository permissions, `up` opens the approval page in the browser and waits up to 10 minutes. Tell the user to approve it there.
- `up` waits for the dotfiles' `setup-ready` marker (up to 20 minutes), then runs `ready_check` until it succeeds (up to 5 minutes), when it is set.
- `up -s` also waits for the dotfiles to write `setup-complete` (up to 30 minutes). Use it when the first command needs tools that the dotfiles install late.
- After `up` prints `ready`, switch this session into the worktree without asking, if your agent can; otherwise run every command from the worktree. Do not edit or run commands in the main checkout; the user keeps it untouched.

## Run a command

From inside the worktree:

```sh
gh csd run <test command> path/to/test
gh csd run   # sync only
```

- `run` uses the codespace that the worktree remembers. `-c <name>` overrides it. Flags go before the command; use `--` if the command starts with `-`.
- `run` refuses a codespace that another worktree owns. `--takeover` claims it anyway. Do not pass that without asking the user.
- The change set is `git diff` against `merge-base(HEAD, origin/<branch>)` plus untracked files, copied on top of the codespace's own checkout, like a rebase. Files dirty in the codespace but no longer in the change set are reset to its HEAD.
- Each run prints `gh csd: copied N, deleted N, reset N`. Report it if the counts look wrong.
- A sync takes a few seconds. `run` returns the remote command's exit status.

### Commands that need GitHub credentials

`run` uses a non-login shell, where `GITHUB_TOKEN` is not set and `~/.local/bin` is not on `PATH`.
Wrap a command in a login shell when it reaches GitHub:

```sh
gh csd run bash -lc 'git fetch origin'
```

- Needs a login shell: `git fetch`, `pull`, `push`, and `ls-remote` on a private repo; `gh` commands; package installs from private sources.
- A login shell adds a second or two per command. Do not use it by default.
- Do not pass the local `gh auth token` into the codespace. The codespace has its own token; only a non-login shell lacks it.

## Tear down an environment

```sh
gh csd down <branch>
```

Always pass the branch: without one, `down` asks for confirmation, which needs a terminal.

- Deletes the worktree's codespace, then removes the worktree and its local branch.
- Refuses when the worktree has uncommitted or untracked files, or the branch has commits that no remote has, and lists them.
- Ask the user before running `down`. Ask again before adding `--force`, which deletes that work.

## Settings

Optional config file: `~/.config/gh-csd/config.yml` (or `$XDG_CONFIG_HOME/gh-csd/config.yml`, or `$GH_CSD_CONFIG`).
Keys go at the top level or under `repos: <owner/name>:`; flags override them.

| Key | Flag | Default |
|---|---|---|
| `machine` | `up -m` | none; `up` asks in a terminal |
| `devcontainer` | `up --devcontainer-path` | `.devcontainer/devcontainer.json` |
| `default_permissions` | `up --default-permissions` | `false`; `true` skips the extra permissions |
| `dir` | | `/workspaces/<repo name>` |
| `branch` | | origin's default branch, else `main`; the codespace's checkout and the base of new worktrees |
| `ready_check` | | none; a command that succeeds once the codespace's services accept connections |

## Check an environment

```sh
gh csd status --json branch,codespace,state,phase
```

Lists each worktree of the repo with its codespace. `state` is the codespace's (`Available`, `Shutdown`, or `Deleted` when it is gone); `phase` is what `up` is doing now.

Do not edit the user's config file without asking.
