# Contributing

gh csd is a Go program built on [go-gh](https://github.com/cli/go-gh). You need Go (the version in `go.mod`).

```sh
make test    # the tests
make check   # gofmt, go vet, the tests, and the build
make build   # ./gh-csd; run it as `gh csd` with `gh extension install .`
```

Run `make check` before you open a pull request. CI runs the same target on macOS and Linux, and pull requests need it to pass.

[AGENTS.md](AGENTS.md) explains the layout, the test fakes, and the names other code depends on.

To release, push a new `v*` tag. CI builds a binary for each platform with [gh-extension-precompile](https://github.com/cli/gh-extension-precompile) and attaches them to a GitHub release. `gh skill install` also takes the skill from the latest release.
