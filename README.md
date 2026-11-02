# gh csd

Edit a GitHub repo in a local worktree, and run it in that worktree's own codespace.

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

`init` writes a commented template to `~/.config/gh-csd/config.yml`. It does not overwrite a file that is already there.

Open the file and set `machine`, the codespace machine type. `up` does not start without it. To list the machine types for a repo, run:

```sh
gh api repos/<owner/name>/codespaces/machines --jq '.machines[].name'
```

For every setting, see [docs/configuration.md](docs/configuration.md).

### 3. Mark when your dotfiles are ready

When `up` creates a codespace, it waits for your dotfiles to write `~/.local/state/gh-csd/setup-ready`. Without that file, `up` waits 20 minutes before it continues. [docs/dotfiles.md](docs/dotfiles.md) has a snippet to add to your dotfiles.

### 4. Start, run, and tear down

Run these from inside your clone or one of its worktrees. A new codespace takes several minutes, so `up` takes a while the first time.

```sh
gh csd up my-feature              # worktree + codespace, waits until both are ready
cd ../my-app-my-feature
# ...edit files locally...
gh csd run bin/test test/user_test.rb
gh csd run                        # sync only
gh csd down my-feature            # refuses if the branch has unsaved work
gh csd down --force my-feature    # deletes it anyway
```

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
