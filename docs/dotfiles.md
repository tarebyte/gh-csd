# Dotfiles markers

When `up` creates a codespace, it waits for two marker files that your [dotfiles](https://docs.github.com/en/codespaces/setting-your-user-preferences/personalizing-github-codespaces-for-your-account#dotfiles) write in the codespace:

| File | What gh csd does with it |
| --- | --- |
| `~/.local/state/gh-csd/setup-ready` | `up` waits up to 20 minutes for this file, then continues without it. |
| `~/.local/state/gh-csd/setup-complete` | gh csd sends a "ready for use" notification when this file appears, up to 30 minutes after `up`. |

Write `setup-ready` once the codespace has what your first `gh csd run` needs. Write `setup-complete` after slower installs that can finish in the background.

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
