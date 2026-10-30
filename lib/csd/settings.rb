module Csd
  # gh csd's settings. Each comes from the first of these that sets it:
  #
  #   1. an environment variable (for one-off overrides)
  #   2. the config file (Config: the repo's section over the top level)
  #   3. the built-in default
  #
  #   config key            environment variable     default
  #   machine               CS_MACHINE               none: up requires it
  #   devcontainer          CS_DEVCONTAINER          .devcontainer/devcontainer.json
  #   default_permissions   CS_DEFAULT_PERMISSIONS   false
  #   dir                   REMOTE_DIR               /workspaces/<repo name>
  #   branch                                         origin's default branch, else main
  #   ready_check           CS_READY_CHECK           none: no services wait
  #
  # Environment only: CS (codespace for run), CS_TAKEOVER (claim a codespace
  # another worktree owns), GH_CSD_CONFIG (config file path).
  class Settings
    DEFAULTS = {
      "devcontainer" => ".devcontainer/devcontainer.json",
      "default_permissions" => "false"
    }.freeze

    ENV_NAMES = {
      "machine" => "CS_MACHINE",
      "devcontainer" => "CS_DEVCONTAINER",
      "default_permissions" => "CS_DEFAULT_PERMISSIONS",
      "dir" => "REMOTE_DIR",
      "ready_check" => "CS_READY_CHECK"
    }.freeze

    KEYS = (ENV_NAMES.values + %w[CS CS_TAKEOVER GH_CSD_CONFIG XDG_CONFIG_HOME TMPDIR HOME]).freeze

    # exe is this binary, which up starts again as the background watcher.
    attr_reader :cwd, :exe #: String

    # Settings for the checkout at cwd, with its origin's repo and default
    # branch ("" for each outside a checkout or without an origin).
    #: (Hash[String, String], String, String, Runner) -> Settings
    def self.for_checkout(env, cwd, exe, runner)
      url = runner.capture(["git", "-C", cwd, "remote", "get-url", "origin"]).out.strip
      head = runner.capture(["git", "-C", cwd, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"]).out.strip
      new(env, cwd, exe, repo: Config.repo_from(url), default_branch: head.sub(%r{\Aorigin/}, ""))
    end

    # repo and default_branch come from the checkout's origin remote.
    #: (Hash[String, String], String, String, ?config: Hash[String, String]?, ?repo: String, ?default_branch: String) -> void
    def initialize(env, cwd, exe, config: nil, repo: "", default_branch: "")
      @env = env
      @cwd = cwd
      @exe = exe
      @repo = repo
      @default_branch = default_branch
      @config = config || Config.load(env, repo)
    end

    #: () -> String
    def repo
      raise Error, "no github.com origin remote in #{@cwd}" if @repo.empty?

      @repo
    end

    #: () -> String
    def machine
      machine = value("machine")
      raise Error, "set machine in #{Config.path(@env)}, or CS_MACHINE" if machine.empty?

      machine
    end

    #: () -> String
    def devcontainer
      value("devcontainer")
    end

    #: () -> bool
    def default_permissions?
      !["", "0", "false"].include?(value("default_permissions"))
    end

    # Where Codespaces clones the repo, unless set.
    #: () -> String
    def remote_dir
      dir = value("dir")
      dir.empty? ? "/workspaces/#{repo.split("/").last}" : dir
    end

    # The codespace's branch, and the base of new worktrees.
    #: () -> String
    def branch
      branch = value("branch")
      return branch unless branch.empty?

      @default_branch.empty? ? "main" : @default_branch
    end

    #: () -> String
    def ready_check
      check = value("ready_check")
      check == "none" ? "" : check
    end

    #: () -> String
    def codespace
      env("CS")
    end

    #: () -> bool
    def takeover?
      !env("CS_TAKEOVER").empty?
    end

    #: () -> String
    def tmpdir
      dir = env("TMPDIR")
      dir.empty? ? "/tmp" : dir
    end

    #: () -> String
    def home
      env("HOME")
    end

    private

    #: (String) -> String
    def value(key)
      from_env = ENV_NAMES[key] ? env(ENV_NAMES[key]) : ""
      return from_env unless from_env.empty?

      @config[key] || DEFAULTS[key].to_s
    end

    #: (String) -> String
    def env(key)
      @env[key].to_s
    end
  end
end
