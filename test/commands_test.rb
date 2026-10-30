require_relative "test_helper"

# Runs whole commands: real git in a throwaway repo, and a fake for gh, ssh,
# rsync, hostname, open, and osascript.
class CommandsTest < Minitest::Test
  include GitRepo

  CONFIG = "Host cs.test.master\n"
  CHECK = "nc -z localhost 5432"
  EXE = TEST_EXE

  def setup
    setup_repo
    @fake = FakeRunner.new.passthrough("git")
    @clock = FakeClock.new
    @waiter = Csd::Waiter.new(@clock)
  end

  def teardown
    teardown_repo
  end

  def commands(env = {}, cwd = @main)
    Csd::Commands.new(@fake, @waiter, csd_settings({ "TMPDIR" => @root, "HOME" => "/Users/me", "CS_READY_CHECK" => CHECK }.merge(env), cwd))
  end

  def tree(branch)
    Csd::Worktree.for_branch(Csd::Runner.new, branch, @main, "master")
  end

  # --- up ---

  def test_up_creates_the_worktree_and_codespace_then_waits_and_syncs
    @fake.on("gh", "codespace", "create", out: "cs-new\n")
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("test -f ~/.local/state/gh-csd/setup-ready", status: 1)

    out, err = capture_io { commands.up("me/feature") }

    wt = tree("me/feature")
    assert wt.exist?
    assert_equal "cs-new", wt.codespace
    assert_equal 15, @clock.now, "polls again after the first ready check fails"
    assert_equal [[EXE, "__watch", "cs-new", "", wt.status_path]], @fake.calls_matching(EXE)
    assert_equal "dotfiles…\n", File.read(wt.status_path), "the watcher clears it later"
    assert_includes @fake.ssh_scripts, "echo :#{wt.path} > /workspaces/my-app/.git/cs-dev-owner"
    assert_includes out, "gh csd: ready  worktree=#{wt.path}  codespace=cs-new"
    assert_includes err, "copied 0, deleted 0, reset 0"
  end

  def test_up_clears_the_status_when_create_fails
    @fake.on("gh", "codespace", "create", err: "there is no such machine for the repository\n", status: 1)

    assert_raises(Csd::Error) { capture_io { commands.up("me/feature") } }

    refute File.exist?(tree("me/feature").status_path)
  end

  def test_up_reuses_a_bound_codespace_that_still_exists
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs-old")
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    _, err = capture_io { commands.up("me/feature") }

    assert_includes err, "reusing worktree"
    assert_includes err, "reusing codespace cs-old"
    assert_empty @fake.calls_matching("gh", "codespace", "create")
    assert_empty @fake.calls_matching(EXE), "no watcher for a reused codespace"
  end

  def test_up_replaces_a_bound_codespace_that_is_gone
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs-gone")
    @fake.on("gh", "codespace", "view", status: 1)
         .on("gh", "codespace", "create", out: "cs-new\n")
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io { commands.up("me/feature") }

    assert_equal "cs-new", wt.codespace
  end

  # Regression: the first test after up failed on a database, which accepts
  # connections a while after the ready marker.
  def test_up_waits_for_the_services_after_the_ready_marker
    @fake.on("gh", "codespace", "create", out: "cs-new\n")
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh(CHECK, status: 1)
         .on_ssh(CHECK, status: 1)

    _, err = capture_io { commands.up("me/feature") }

    assert_includes err, "waiting for services..."
    assert_equal 10, @clock.now, "two failed service checks, 5s apart"
    assert_equal 3, @fake.ssh_scripts.count(CHECK)
  end

  def test_up_with_ready_check_none_skips_the_services_wait
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)

    _, err = capture_io { commands("CS_READY_CHECK" => "none").up("me/feature") }

    refute_includes err, "waiting for services"
    refute(@fake.ssh_scripts.any? { |script| script.include?("nc -z") })
  end

  def test_up_continues_when_the_services_never_come_up
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)
    61.times { @fake.on_ssh("nc -z", status: 1) }

    out, err = capture_io { commands.up("me/feature") }

    assert_includes err, "services not ready after 5m; continuing"
    assert_includes out, "gh csd: ready"
  end

  # Regression: a codespace that shut down while idle restarted with its
  # database still starting, and the first test failed to connect.
  def test_up_waits_for_the_services_on_a_reused_codespace
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs-old")
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh(CHECK, status: 1)

    _, err = capture_io { commands.up("me/feature") }

    assert_includes err, "waiting for services..."
    assert_equal 2, @fake.ssh_scripts.count(CHECK)
    refute File.exist?(wt.status_path), "clears the status a reused codespace never hands to a watcher"
  end

  def test_up_passes_settings_to_create
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io do
      commands("CS_MACHINE" => "big-machine", "CS_DEFAULT_PERMISSIONS" => "1").up("me/feature")
    end

    cmd = @fake.calls_matching("gh", "codespace", "create").first
    assert_includes cmd, "big-machine"
    assert_includes cmd, "--default-permissions"
  end

  # --- init ---

  def test_init_writes_a_template_that_parses_to_no_settings
    @fake.passthrough("mkdir")
    path = "#{@root}/config/gh-csd/config.yml"

    _, err = capture_io { commands("GH_CSD_CONFIG" => path).init }

    assert_includes err, "wrote #{path}"
    assert_equal Csd::Config::TEMPLATE, File.read(path)
    assert_equal({}, Csd::Config.parse(File.read(path), path, "me/my-app"))
  end

  def test_init_never_replaces_an_existing_config
    path = "#{@root}/config.yml"
    File.write(path, "machine: mine\n")

    error = assert_raises(Csd::Error) { commands("GH_CSD_CONFIG" => path).init }
    assert_match "already exists", error.message
    assert_equal "machine: mine\n", File.read(path)
  end

  # --- run ---

  def test_run_copies_deletes_and_resets_then_runs_the_command
    File.write("#{@main}/doomed.rb", "1")
    sh("git", "-C", @main, "add", ".")
    sh("git", "-C", @main, "commit", "-q", "-m", "doomed")
    sh("git", "-C", @main, "push", "-q", "origin", "HEAD:master")
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs1")
    File.delete("#{wt.path}/doomed.rb")
    File.write("#{wt.path}/new.rb", "2")
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("git status --porcelain", out: " M stray.rb\0 D doomed.rb\0")
         .on_ssh("bin/test", status: 3)

    status = nil
    _, err = capture_io { status = commands({}, wt.path).run(["bin/test", "test/x.rb"]) }

    assert_equal 3, status, "returns the remote command's exit status"
    assert_includes err, "copied 1, deleted 1, reset 1"
    assert_equal "new.rb\0", @fake.stdin_of { |cmd| cmd.first == "rsync" }
    assert_equal "#{wt.path}/", @fake.calls_matching("rsync").first[-2]
    assert_equal "doomed.rb\0", @fake.stdin_of { |cmd| cmd.first == "ssh" && cmd.last.include?("rm -f") }
    assert_equal "stray.rb\0", @fake.stdin_of { |cmd| cmd.first == "ssh" && cmd.last.include?("git cat-file") }
    assert_includes @fake.calls_matching("ssh").last, "-t"
  end

  def test_run_with_no_command_only_syncs
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs1")
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io { assert_equal 0, commands({}, wt.path).run([]) }

    refute(@fake.calls_matching("ssh").any? { |cmd| cmd.include?("-t") })
  end

  # Regression: the shell version saved the binding before the owner check,
  # so a refused run still rebound the worktree.
  def test_run_refuses_a_codespace_another_worktree_owns_and_keeps_the_binding
    wt = tree("me/feature").tap(&:create)
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("cat ", out: "other-mac:/src/github-other\n")

    error = assert_raises(Csd::Error) { commands({ "CS" => "cs-taken" }, wt.path).run([]) }

    assert_match "in use by other-mac:/src/github-other", error.message
    assert_nil wt.codespace
    assert_empty @fake.calls_matching("rsync")
  end

  def test_run_takeover_claims_the_codespace
    wt = tree("me/feature").tap(&:create)
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("cat ", out: "other-mac:/src/github-other\n")

    capture_io { commands({ "CS" => "cs-taken", "CS_TAKEOVER" => "1" }, wt.path).run([]) }

    assert_equal "cs-taken", wt.codespace
  end

  def test_run_without_a_codespace_says_to_run_up
    wt = tree("me/feature").tap(&:create)

    error = assert_raises(Csd::Error) { commands({}, wt.path).run([]) }
    assert_match "gh csd up", error.message
  end

  def test_run_stops_when_rsync_fails
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs1")
    File.write("#{wt.path}/new.rb", "1")
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG).on("rsync", err: "connection closed", status: 12)

    error = assert_raises(Csd::Error) { capture_io { commands({}, wt.path).run(["true"]) } }
    assert_match "rsync failed", error.message
    refute(@fake.calls_matching("ssh").any? { |cmd| cmd.include?("-t") })
  end

  # --- down ---

  def test_down_refuses_to_lose_work
    wt = tree("me/feature").tap(&:create)
    File.write("#{wt.path}/new.rb", "1")

    error = assert_raises(Csd::Error) { commands.down("me/feature") }

    assert_match(/uncommitted:\n\?\? new.rb/, error.message)
    assert wt.exist?
    assert_empty @fake.calls_matching("gh", "codespace", "delete")
  end

  def test_down_deletes_the_codespace_worktree_branch_and_cached_config
    wt = tree("me/feature").tap(&:create)
    wt.bind("cs1")
    File.write("#{@root}/gh-csd-cs1.ssh", CONFIG)

    capture_io { commands.down("me/feature") }

    assert_equal [["gh", "codespace", "delete", "-c", "cs1", "--force"]], @fake.calls_matching("gh", "codespace", "delete")
    refute wt.exist?
    refute File.exist?("#{@root}/gh-csd-cs1.ssh")
    assert_empty sh("git", "-C", @main, "branch", "--list", "me/feature")
  end

  def test_down_force_deletes_a_dirty_worktree
    wt = tree("me/feature").tap(&:create)
    File.write("#{wt.path}/new.rb", "1")

    capture_io { commands.down("me/feature", force: true) }

    refute wt.exist?
  end

  def test_down_without_a_worktree_raises
    assert_raises(Csd::Error) { commands.down("me/nothing") }
  end

  # --- watch ---

  def test_watch_notifies_once_dotfiles_are_done_and_clears_the_status
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("test -f ~/.local/state/gh-csd/setup-complete", status: 1)
    File.write("#{@root}/status", "dotfiles…\n")

    commands.watch("cs1", "", "#{@root}/status")

    assert_equal 30, @clock.now
    notify = @fake.calls_matching("osascript").first
    assert_includes notify.last, "Dotfiles setup finished in cs1"
    refute File.exist?("#{@root}/status")
  end

  def test_watch_gives_up_quietly_after_30_minutes_and_clears_the_status
    @fake.on("gh", "codespace", "ssh", "--config", status: 1)
    61.times { @fake.on("gh", "codespace", "ssh", "--config", status: 1) }
    File.write("#{@root}/status", "dotfiles…\n")

    commands.watch("cs1", "", "#{@root}/status")

    assert_equal 1800, @clock.now
    assert_empty @fake.calls_matching("osascript")
    refute File.exist?("#{@root}/status")
  end
end
