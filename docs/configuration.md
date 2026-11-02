# Configuration

Set settings in a config file or in environment variables. An environment variable wins over the config file. `gh csd init` writes a template with every setting.

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

| Config key | Environment variable | Default |
| --- | --- | --- |
| `machine` | `CS_MACHINE` | none; `up` requires it |
| `devcontainer` | `CS_DEVCONTAINER` | `.devcontainer/devcontainer.json` |
| `default_permissions` | `CS_DEFAULT_PERMISSIONS` | `false` |
| `dir` | `REMOTE_DIR` | `/workspaces/<repo name>` |
| `branch` | | origin's default branch, else `main` |
| `ready_check` | `CS_READY_CHECK` | none (no wait) |

To list the machine types for a repo, run:

```sh
gh api repos/<owner/name>/codespaces/machines --jq '.machines[].name'
```

`default_permissions: true` skips the devcontainer's request for extra repository permissions, so `up` does not open your browser to approve them.

`ready_check` is a command that `up` runs in the codespace every 5 seconds, for up to 5 minutes, until it succeeds. Use it when your services take a while to accept connections after the codespace starts. `none` skips it.

## Environment only

- `CS`: the codespace for `run`, instead of the worktree's own.
- `CS_TAKEOVER`: let `run` claim a codespace that another worktree owns.
