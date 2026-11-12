require "json"

module Csd
  class Commands
    READY = "~/.local/state/gh-csd/setup-ready"
    DONE = "~/.local/state/gh-csd/setup-complete"

    #: (Runner, Waiter, Settings, Prompt) -> void
    def initialize(runner, waiter, settings, prompt)
      @runner = runner
      @waiter = waiter
      @settings = settings
      @prompt = prompt
      @codespaces = Codespace.new(runner, Notifier.new(runner), waiter)
    end

    # branch "" means the worktree at cwd. With status, also waits for the
    # dotfiles to finish (setup-complete).
    #: (String, ?status: bool) -> void
    def up(branch, status: false)
      tree = worktree(branch)
      log "reusing worktree #{tree.path}" unless tree.create
      name = reusable_codespace(tree) || create_codespace(tree, status)
      sync(tree, name)
      puts "gh csd: ready  worktree=#{tree.path}  codespace=#{name}"
    end

    #: (Array[String]) -> Integer
    def run(args)
      tree = Worktree.current(@runner, @settings.cwd, @settings.branch)
      remote = sync(tree, codespace_for(tree))
      args.empty? ? 0 : remote.run(args)
    end

    # A table, or with fields (comma-separated), JSON as gh prints it.
    #: (?String) -> void
    def status(fields = "")
      rows = status_rows
      return puts(JSON.pretty_generate(select_fields(rows, fields))) unless fields.empty?
      return warn("gh csd: no worktrees") if rows.empty?

      rows.each { |row| puts [row["branch"], row["codespace"], row["state"], row["phase"], row["worktree"]].join("\t") }
    end

    # branch "" means the worktree at cwd, after asking.
    #: (String, ?force: bool) -> void
    def down(branch, force: false)
      tree = branch.empty? ? confirmed_here : Worktree.for_branch(@runner, branch, @settings.cwd, @settings.branch)
      refuse_to_lose_work(tree, tree.branch.to_s, force)
      delete_codespace(tree.codespace)
      tree.remove(force: force)
      log "removed worktree=#{tree.path} branch=#{tree.branch}"
    end

    # Writes the commented template config; never replaces an existing file.
    #: () -> void
    def init
      path = @settings.config_path
      raise Error, "#{path} already exists" if File.exist?(path)

      @runner.capture(["mkdir", "-p", File.dirname(path)])
      File.write(path, Config::TEMPLATE)
      log "wrote #{path}"
    end

    private

    #: (String) -> Worktree
    def worktree(branch)
      return Worktree.here(@runner, @settings.cwd, @settings.branch) if branch.empty?

      Worktree.for_branch(@runner, branch, @settings.cwd, @settings.branch)
    end

    #: () -> Worktree
    def confirmed_here
      raise Error, "pass the branch to down when not in a terminal" unless @prompt.interactive?

      tree = Worktree.here(@runner, @settings.cwd, @settings.branch)
      what = "worktree #{tree.path} and branch #{tree.branch}"
      what = "codespace #{tree.codespace}, #{what}" if tree.codespace
      raise Error, "cancelled" unless @prompt.confirm?("Delete #{what}?")

      tree
    end

    #: (Worktree) -> String?
    def reusable_codespace(tree)
      name = tree.codespace
      return nil unless name && @codespaces.exist?(name)

      log "reusing codespace #{name}"
      # A stopped codespace restarts on connect, and its services start after it.
      wait_for_services(tree, name)
      tree.status = ""
      name
    end

    #: (Worktree, bool) -> String
    def create_codespace(tree, status)
      choose_machine
      log "creating codespace on #{@settings.machine} (a few minutes)..."
      tree.status = "creating…"
      name = @codespaces.create(tree.branch.to_s, @settings)
      tree.bind(name)
      wait_until_ready(tree, name, status)
      name
    ensure
      tree.status = ""
    end

    # As gh codespace create: the only machine type without asking, else
    # ask in a terminal, else fail with the flag to pass.
    #: () -> void
    def choose_machine
      return unless @settings.machine.empty?

      names = @codespaces.machines(@settings)
      raise Error, "#{@settings.repo} has no machine types available" if names.empty?
      if names.size == 1
        @settings.machine = names[0].to_s
      elsif @prompt.interactive?
        @settings.machine = @prompt.choose("Choose a machine type:", names)
      else
        raise Error, "pass -m with one of: #{names.join(", ")}"
      end
    end

    #: (Worktree, String, bool) -> void
    def wait_until_ready(tree, name, status)
      log "waiting for codespace setup..."
      tree.status = "setting up…"
      ready = @waiter.wait(1200, 15) { remote(name).file?(READY) }
      log "no ready marker after 20m; continuing" unless ready
      wait_for_services(tree, name)
      wait_for_dotfiles(tree, name) if status
    end

    #: (Worktree, String) -> void
    def wait_for_dotfiles(tree, name)
      log "waiting for the dotfiles to finish..."
      tree.status = "dotfiles…"
      done = @waiter.wait(1800, 30) { remote(name).file?(DONE) }
      log(done ? "dotfiles finished" : "dotfiles not finished after 30m; continuing")
    end

    # Services start with the container but take a while to accept
    # connections: the first test after up can fail without this.
    #: (Worktree, String) -> void
    def wait_for_services(tree, name)
      check = @settings.ready_check
      return if check.empty?

      log "waiting for services..."
      tree.status = "starting services…"
      ready = @waiter.wait(300, 5) { remote(name).capture(check).success? }
      log "services not ready after 5m; continuing" unless ready
    end

    #: () -> Array[Hash[String, String]]
    def status_rows
      states = @codespaces.states(@settings)
      Worktree.all(@runner, @settings.cwd, @settings.branch).map do |tree|
        name = tree.codespace.to_s
        state = name.empty? ? "" : (states[name] || "Deleted")
        { "worktree" => tree.path, "branch" => tree.branch.to_s, "base" => @settings.branch,
          "codespace" => name, "state" => state, "phase" => tree.phase }
      end
    end

    #: (Array[Hash[String, String]], String) -> Array[Hash[String, String]]
    def select_fields(rows, fields)
      wanted = fields.split(",").map { |field| field.strip }
      unknown = wanted - Cli::JSON_FIELDS
      raise UsageError, "unknown JSON field #{unknown.join(", ")}; use #{Cli::JSON_FIELDS.join(", ")}" unless unknown.empty?

      rows.map { |row| row.slice(*wanted) }
    end

    #: (Worktree) -> String
    def codespace_for(tree)
      name = @settings.codespace
      name = tree.codespace.to_s if name.empty?
      raise Error, "no codespace for #{tree.path}; run gh csd up first, or pass -c" if name.empty?

      name
    end

    #: (Worktree, String, bool) -> void
    def refuse_to_lose_work(tree, branch, force)
      raise Error, "no worktree at #{tree.path}" unless tree.exist?
      return if force

      work = tree.unsaved_work
      raise Error, "#{branch} has work that would be lost; --force deletes it anyway\n#{work}" unless work.empty?
    end

    #: (String?) -> void
    def delete_codespace(name)
      return unless name && @codespaces.exist?(name)

      @codespaces.delete(name)
      remote(name).forget
      log "deleted codespace #{name}"
    end

    #: (Worktree, String) -> Remote
    def sync(tree, name)
      remote = remote(name)
      Sync.new(@runner, remote, @settings.takeover?).call(tree)
      remote
    end

    #: (String) -> Remote
    def remote(name)
      Remote.new(@runner, name, @settings)
    end

    #: (String) -> void
    def log(message)
      warn "gh csd: #{message}"
    end
  end
end
