# gh csd

Edit a GitHub repo in a local worktree, and run it in that worktree's own codespace.

- `gh csd up` makes a codespace for the worktree you are in. `gh csd up <branch>` uses that branch's worktree, or makes one next to your main checkout (`~/src/my-app` → `~/src/my-app-<branch>`).
- `gh csd run <command>` copies the worktree's changes to the codespace with rsync, then runs the command there over SSH.
- `gh csd status` lists each worktree and its codespace. Add `--json <fields>`, with `--jq` or `--template`, for scripts, as in `gh`.
- `gh csd down` deletes the codespace, the worktree, and the branch.

The codespace checks out the base branch (origin's default branch). `run` copies your changes on top of it; it never checks out your branch there.

Each worktree has its own codespace, so you can work on several branches at once.

## Requirements

- macOS or Linux.
- `gh`, logged in with the `codespace` scope: `gh auth refresh -s codespace`.
- `rsync` and `ssh`.
- A local clone of the repo, with its `origin` remote on github.com.

## Get started

### 1. Install

```sh
gh extension install tarebyte/gh-csd
```

Upgrade later with `gh extension upgrade csd`.

### 2. Optional: create a config file

```sh
gh csd init
```

`init` writes a commented template to `~/.config/gh-csd/config.yml`. It does not overwrite a file that is already there.

Set `machine` there to skip the machine type question. Without it, `up` asks in a terminal, or takes `-m <machine>`. For every setting, see [docs/configuration.md](docs/configuration.md).

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
gh csd status                     # each worktree, its codespace, and its state
gh csd down                       # asks, then deletes this worktree's codespace, worktree, and branch
gh csd down --force my-feature    # deletes it even with unsaved work
```

Already in a worktree, for example one your editor or another tool made? Run `gh csd up` there. Use `gh csd <command> --help` for every flag.

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
