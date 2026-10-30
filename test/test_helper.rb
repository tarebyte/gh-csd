require "minitest/autorun"
require "tempfile"
require_relative "../lib/csd"
require_relative "support/fake_runner"
require_relative "support/git_repo"
require_relative "support/fake_clock"

TEST_EXE = "/usr/local/bin/gh-csd"

# Settings for tests. env needs TMPDIR (where SSH configs are cached) and
# HOME (which names the ControlPath). config defaults to empty, so tests
# never read the real ~/.config/gh-csd/config.yml. The origin is
# me/my-app, whose default branch is master.
def csd_settings(env, cwd = "/", config = { "machine" => "test-machine" })
  Csd::Settings.new(env, cwd, TEST_EXE, config: config, repo: "me/my-app", default_branch: "master")
end
