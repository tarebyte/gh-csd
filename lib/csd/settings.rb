module Csd
  # gh csd's settings. Each comes from the first of these that sets it:
  #
  #   1. a flag
  #   2. the config file (Config: the repo's section over the top level)
  #   3. the built-in default
  #
  #   config key            flag                    default
  #   machine               -m, --machine           none: up asks in a terminal
  #   devcontainer          --devcontainer-path     .devcontainer/devcontainer.json
  #   default_permissions   --default-permissions   false
  #   dir                                           /workspaces/<repo name>
  #   branch                                        origin's default branch, else main
  #   ready_check                                   none: no services wait
  #
  # Flags only: codespace (-c, for run) and takeover (claim a codespace
  # another worktree owns). The environment only locates files:
  # GH_CSD_CONFIG, XDG_CONFIG_HOME, TMPDIR, HOME.
  class Settings
    DEFAULTS = {
      "devcontainer" => ".devcontainer/devcontainer.json",
      "default_permissions" => "false"
    }.freeze

    KEYS = %w[GH_CSD_CONFIG XDG_CONFIG_HOME TMPDIR HOME].freeze

    attr_reader :cwd #: String

    # Settings for the checkout at cwd, with its origin's repo and default
    # branch ("" for each outside a checkout or without an origin).
    #: (Hash[String, String], String, Hash[String, String], Runner) -> Settings
    def self.for_checkout(env, cwd, flags, runner)
      url = runner.capture(["git", "-C", cwd, "remote", "get-url", "origin"]).out.strip
      head = runner.capture(["git", "-C", cwd, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"]).out.strip
      new(env, cwd, flags: flags, repo: Config.repo_from(url), default_branch: head.sub(%r{\Aorigin/}, ""))
    end

    # repo and default_branch come from the checkout's origin remote.
    #: (Hash[String, String], String, ?flags: Hash[String, String], ?config: Hash[String, String]?, ?repo: String, ?default_branch: String) -> void
    def initialize(env, cwd, flags: {}, config: nil, repo: "", default_branch: "")
      @env = env
      @cwd = cwd
      @flags = flags.dup
      @repo = repo
      @default_branch = default_branch
      @config = config || Config.load(env, repo)
    end

    #: () -> String
    def repo
      raise Error, "no github.com origin remote in #{@cwd}" if @repo.empty?

      @repo
    end

    # "" when neither -m nor the config file sets it: up asks, or fails.
    #: () -> String
    def machine
      value("machine")
    end

    #: (String) -> void
    def machine=(name)
      @flags["machine"] = name
    end

    #: () -> String
    def config_path
      Config.path(@env)
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
      @flags["codespace"].to_s
    end

    #: () -> bool
    def takeover?
      @flags["takeover"].to_s == "true"
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
      flag = @flags[key].to_s
      return flag unless flag.empty?

      @config[key] || DEFAULTS[key].to_s
    end

    #: (String) -> String
    def env(key)
      @env[key].to_s
    end
  end
end
