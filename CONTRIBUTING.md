# Contributing

gh csd is Ruby compiled to a native binary with [Spinel](https://github.com/matz/spinel). You need Spinel to build. The Makefile looks for it at `~/src/matz/spinel/spinel`; set `SPINEL` to change that.

```sh
make test    # unit tests on CRuby
make check   # tests, spinel diff, type check, and build
```

Run `make check` before you open a pull request. CI runs the same target, and pull requests need it to pass.

[AGENTS.md](AGENTS.md) explains the Spinel rules, the test fakes, and the names other code depends on.

To release, push a new `v*` tag. CI builds `gh-csd-darwin-arm64` and attaches it to a GitHub release. `gh skill install` also takes the skill from the latest release.
