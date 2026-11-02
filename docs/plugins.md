# Install as a plugin

The README installs the `codespace-dev` skill with `gh skill`, which works with most agents. Claude Code and GitHub Copilot CLI can also install this repo as a plugin, which includes the same skill.

## Claude Code

```sh
claude plugin marketplace add tarebyte/gh-csd
claude plugin install gh-csd@gh-csd
```

Update with `claude plugin marketplace update gh-csd`. Start the skill directly with `/gh-csd:codespace-dev`.

## GitHub Copilot CLI

```sh
copilot plugin marketplace add tarebyte/gh-csd
copilot plugin install gh-csd@gh-csd
```

If you run Copilot through `gh`, use `gh copilot -- plugin …` instead of `copilot plugin …`.
