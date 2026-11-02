module Csd
  class Commands
    READY = "~/.local/state/gh-csd/setup-ready"
    DONE = "~/.local/state/gh-csd/setup-complete"

    #: (Runner, Waiter, Settings) -> void
    def initialize(runner, waiter, settings)
      @runner = runner
      @waiter = waiter
      @settings = settings
      @notifier = Notifier.new(runner)
      @codespaces = Codespace.new(runner, @notifier, waiter)
    end

    #: (String) -> void
    def up(branch)
      tree = Worktree.for_branch(@runner, branch, @settings.cwd, @settings.branch)
      log "reusing worktree #{tree.path}" unless tree.create
      name = reusable_codespace(tree) || create_codespace(tree, branch)
      sync(tree, name)
      puts "gh csd: ready  worktree=#{tree.path}  codespace=#{name}"
    end

    #: (Array[String]) -> Integer
    def run(args)
      tree = Worktree.current(@runner, @settings.cwd, @settings.branch)
      remote = sync(tree, codespace_for(tree))
      args.empty? ? 0 : remote.run(args)
    end

    #: (String, ?force: bool) -> void
    def down(branch, force: false)
      tree = Worktree.for_branch(@runner, branch, @settings.cwd, @settings.branch)
      refuse_to_lose_work(tree, branch, force)
      delete_codespace(tree.codespace)
      tree.remove(force: force)
      log "removed worktree=#{tree.path} branch=#{branch}"
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

    # Hidden: up starts it in the background to notify once the dotfiles
    # finish (setup-complete), then clear the worktree's status.
    # tty is up's iTerm2 tty, or "".
    #: (String, String, String) -> void
    def watch(name, tty, status_path)
      done = @waiter.wait(1800, 30) { remote(name).file?(DONE) }
      @notifier.notify("gh csd: ready for use", "Dotfiles setup finished in #{name}", tty: tty) if done
      Worktree.write_status(status_path, "")
    end

    private

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

    #: (Worktree, String) -> String
    def create_codespace(tree, branch)
      log "creating codespace (a few minutes)..."
      tree.status = "creating…"
      name = @codespaces.create(branch, @settings)
      tree.bind(name)
      wait_until_ready_then_watch(tree, name)
      name
    rescue StandardError
      tree.status = ""
      raise
    end

    #: (Worktree, String) -> void
    def wait_until_ready_then_watch(tree, name)
      log "waiting for codespace setup..."
      tree.status = "setting up…"
      ready = @waiter.wait(1200, 15) { remote(name).file?(READY) }
      log "no ready marker after 20m; continuing" unless ready
      wait_for_services(tree, name)
      tree.status = "dotfiles…"
      @runner.spawn_detached([@settings.exe, "__watch", name, @notifier.iterm_tty, tree.status_path])
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

    #: (Worktree) -> String
    def codespace_for(tree)
      name = @settings.codespace
      name = tree.codespace.to_s if name.empty?
      raise Error, "no codespace for #{tree.path}; run gh csd up first, or set CS" if name.empty?

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
