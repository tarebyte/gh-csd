module Csd
  class Codespace
    APPROVAL_MESSAGE = "authorize or deny additional permissions"

    #: (Runner, Notifier, Waiter) -> void
    def initialize(runner, notifier, waiter)
      @runner = runner
      @notifier = notifier
      @waiter = waiter
    end

    #: (String) -> bool
    def exist?(name)
      @runner.capture(["gh", "codespace", "view", "-c", name]).success?
    end

    # The devcontainer can ask for extra repository permissions. Until the
    # user approves them, gh (without a terminal) prints an approval URL and
    # fails: open it, wait for the approval, and create again.
    #: (String, Settings) -> String
    def create(display_name, settings)
      args = create_args(display_name, settings)
      result = @runner.capture(args)
      result = approve_and_retry(args, result, settings) if approval_needed?(result)
      name_from(result) || raise(Error, "gh codespace create failed: #{result.err.strip}")
    end

    #: (Settings) -> bool
    def permissions_accepted?(settings)
      result = @runner.capture(["gh", "api", "-X", "GET", "repos/#{settings.repo}/codespaces/permissions_check",
                                "-f", "ref=#{settings.branch}", "-f", "devcontainer_path=#{settings.devcontainer}",
                                "--jq", ".accepted"])
      result.success? && result.out.strip == "true"
    end

    # The repo's machine types, as -m takes them.
    #: (Settings) -> Array[String]
    def machines(settings)
      result = @runner.capture(["gh", "api", "repos/#{settings.repo}/codespaces/machines",
                                "--jq", ".machines[].name"])
      raise Error, "listing machine types failed: #{result.err.strip}" unless result.success?

      result.out.split("\n").map { |line| line.strip }.reject { |line| line.empty? }
    end

    # name => state (Available, Shutdown, ...) of the repo's codespaces.
    #: (Settings) -> Hash[String, String]
    def states(settings)
      result = @runner.capture(["gh", "codespace", "list", "-R", settings.repo, "--json", "name,state",
                                "--jq", ".[] | .name + \" \" + .state"])
      raise Error, "gh codespace list failed: #{result.err.strip}" unless result.success?

      found = {}
      result.out.split("\n").each do |line|
        name, state = line.strip.split(" ", 2)
        found[name.to_s] = state.to_s unless name.to_s.empty?
      end
      found
    end

    #: (String) -> void
    def delete(name)
      result = @runner.capture(["gh", "codespace", "delete", "-c", name, "--force"])
      raise Error, "gh codespace delete failed: #{result.err.strip}" unless result.success?
    end

    private

    #: (String, Settings) -> Array[String]
    def create_args(display_name, settings)
      args = ["gh", "codespace", "create", "-R", settings.repo, "-b", settings.branch, "-m", settings.machine,
              "--devcontainer-path", settings.devcontainer, "-d", display_name[0, 48]]
      args << "--default-permissions" if settings.default_permissions?
      args
    end

    #: (Result) -> bool
    def approval_needed?(result)
      name_from(result).nil? && result.err.include?(APPROVAL_MESSAGE)
    end

    #: (Array[String], Result, Settings) -> Result
    def approve_and_retry(args, result, settings)
      approve(result.err[%r{https://\S+}], settings)
      @runner.capture(args)
    end

    # gh prints progress first and the new codespace's name last.
    #: (Result) -> String?
    def name_from(result)
      return nil unless result.success?

      result.out.split("\n").map { |line| line.strip }.reject { |line| line.empty? }.last
    end

    #: (String?, Settings) -> void
    def approve(url, settings)
      raise Error, "gh asked for a permissions approval without a URL" if url.nil?

      warn "gh csd: approve the codespace's extra permissions in your browser: #{url}"
      @notifier.open(url)
      approved = @waiter.wait(600, 5) { permissions_accepted?(settings) }
      raise Error, "no approval after 10 minutes; run up again, or pass --default-permissions" unless approved
    end
  end
end
