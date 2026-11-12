# Configuration

Set settings with flags or in a config file. A flag wins over the config file. `gh csd init` writes a template with every setting.

## Config file

gh csd reads the first of:

1. `$GH_CSD_CONFIG`
2. `$XDG_CONFIG_HOME/gh-csd/config.yml`
3. `~/.config/gh-csd/config.yml`

Put keys at the top level, or under `repos: <owner/name>:` for one repo. A repo's section wins over the top level:

```yaml
machine: standardLinux32gb
repos:
  my-org/my-app:
    machine: premiumLinux
    ready_check: nc -z localhost 5432
```

## Settings

| Config key | Flag | Default |
| --- | --- | --- |
| `machine` | `up -m, --machine` | none; `up` asks in a terminal, and picks the only one without asking |
| `devcontainer` | `up --devcontainer-path` | `.devcontainer/devcontainer.json` |
| `default_permissions` | `up --default-permissions` | `false` |
| `dir` | | `/workspaces/<repo name>` |
| `branch` | | origin's default branch, else `main` |
| `ready_check` | | none (no wait) |

`branch` is the base: the codespace checks it out, and new worktrees start from it.

`default_permissions: true` skips the devcontainer's request for extra repository permissions, so `up` does not open your browser to approve them.

`ready_check` is a command that `up` runs in the codespace every 5 seconds, for up to 5 minutes, until it succeeds. Use it when your services take a while to accept connections after the codespace starts. `none` skips it.

## Flags only

- `run -c, --codespace <name>`: use that codespace instead of the worktree's own.
- `run --takeover`: claim a codespace that another worktree owns.

## Upgrading from 0.1

0.2 drops the environment variables. Use the flag or config key instead:

| 0.1 | 0.2 |
| --- | --- |
| `CS_MACHINE` | `-m` or `machine` |
| `CS_DEVCONTAINER` | `--devcontainer-path` or `devcontainer` |
| `CS_DEFAULT_PERMISSIONS` | `--default-permissions` or `default_permissions` |
| `REMOTE_DIR` | `dir` |
| `CS_READY_CHECK` | `ready_check` |
| `CS` | `run -c` |
| `CS_TAKEOVER` | `run --takeover` |

`up` no longer leaves a background notifier for the dotfiles. Pass `-s` to wait for them in the foreground.
