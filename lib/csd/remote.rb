require "shellwords"

module Csd
  class Error < StandardError; end
  # Retrying never helps: the devcontainer has to install sshd.
  class NoSshServer < Error; end

  # Talks to a codespace over plain ssh and rsync, using the config that
  # `gh codespace ssh --config` prints.
  class Remote
    attr_reader :codespace, :dir #: String

    #: (Runner, String, Settings) -> void
    def initialize(runner, codespace, settings)
      @runner = runner
      @codespace = codespace
      @dir = settings.remote_dir
      @cached_config = File.join(settings.tmpdir, "gh-csd-#{codespace}.ssh")
      @home = settings.home
    end

    # Cached across runs: fetching it goes through gh and costs seconds.
    #: () -> String
    def config_path
      fetch_config unless File.exist?(@cached_config) && File.size(@cached_config) > 0
      @cached_config
    end

    # Written without `@host ||= ... || raise`: with that form, Spinel
    # 2026.10.25 returned config_path on the first call (spinel/smoke.rb
    # caught it), though a reduced copy compiled correctly.
    #: () -> String
    def host
      @host ||= parse_host
    end

    #: () -> void
    def forget
      File.delete(@cached_config) if File.exist?(@cached_config)
    end

    # IdentityAgent=none skips the local SSH agent (1Password), which adds
    # about 60s to every new connection before ssh falls back to the
    # codespaces key. ControlMaster shares one connection across runs.
    #: () -> Array[String]
    def ssh_command
      ["ssh", "-F", config_path,
       "-o", "IdentityAgent=none",
       "-o", "ControlMaster=auto",
       "-o", "ControlPath=#{@home}/.ssh/gh-csd-%C",
       "-o", "ControlPersist=15m"]
    end

    #: (String, ?stdin: String) -> Result
    def capture(script, stdin: "")
      @runner.capture(ssh_command + [host, script], stdin: stdin)
    end

    # A codespace that is still starting fails SSH, or even the config
    # fetch; both count as "not yet", so wait loops keep polling. One with
    # no SSH server never gets there, so that raises.
    #: (String) -> bool
    def file?(path)
      capture("test -f #{path}").success?
    rescue NoSshServer
      raise
    rescue Error
      false
    end

    # Each argument reaches the remote shell as one word.
    #: (Array[String]) -> Integer
    def run(args)
      @runner.run(ssh_command + ["-t", host, "cd #{Shellwords.escape(@dir)} && #{Shellwords.join(args)}"])
    end

    #: (Array[String], String) -> Result
    def copy(paths, from_dir)
      return Result.new("", "", 0) if paths.empty?

      @runner.capture(["rsync", "-a", "--from0", "--files-from=-", "-e", rsync_shell,
                       "#{from_dir}/", "#{host}:#{@dir}/"], stdin: paths.join("\0") + "\0")
    end

    # rsync splits -e on whitespace itself rather than through a shell, so
    # join with plain spaces and refuse words it would split.
    #: () -> String
    def rsync_shell
      words = ssh_command
      bad = words.find { |word| word.match?(/\s/) }
      raise Error, "rsync cannot pass #{bad.inspect} to ssh: it contains whitespace" if bad

      words.join(" ")
    end

    private

    #: () -> String
    def parse_host
      found = File.read(config_path)[/^Host\s+(\S+)/, 1]
      raise Error, "no Host in #{config_path}" if found.nil?

      found
    end

    #: () -> void
    def fetch_config
      result = @runner.capture(["gh", "codespace", "ssh", "--config", "-c", @codespace])
      if result.err.include?("failed to start SSH server")
        raise NoSshServer, "#{@codespace} has no SSH server: add \"ghcr.io/devcontainers/features/sshd:1\": {} to the devcontainer's features, then rebuild it"
      end
      raise Error, "gh codespace ssh --config failed: #{result.err.strip}" unless result.success? && !result.out.empty?

      File.write(@cached_config, result.out)
    end
  end
end
