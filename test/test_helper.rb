require "minitest/autorun"
require "tempfile"
require "stringio"
require_relative "../lib/csd"
require_relative "support/fake_runner"
require_relative "support/git_repo"
require_relative "support/fake_clock"

# Settings for tests. env needs TMPDIR (where SSH configs are cached) and
# HOME (which names the ControlPath). config defaults to empty, so tests
# never read the real ~/.config/gh-csd/config.yml. flags are what the
# command line set. The origin is me/my-app, whose default branch is master.
def csd_settings(env, cwd = "/", config = { "machine" => "test-machine" }, flags = {})
  Csd::Settings.new(env, cwd, flags: flags, config: config, repo: "me/my-app", default_branch: "master")
end

# A prompt with scripted answers; interactive unless told otherwise.
def csd_prompt(answers = "", interactive: true)
  Csd::Prompt.new(StringIO.new(answers), StringIO.new, interactive)
end
