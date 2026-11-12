require_relative "test_helper"

# Runs whole commands: real git in a throwaway repo, and a fake for gh, ssh,
# rsync, hostname, and open.
class CommandsTest < Minitest::Test
  include GitRepo

  CONFIG = "Host cs.test.master\n"
  CHECK = "nc -z localhost 5432"

  def setup
    setup_repo
    @fake = FakeRunner.new.passthrough("git")
    @clock = FakeClock.new
    @waiter = Csd::Waiter.new(@clock)
  end

  def teardown
    teardown_repo
  end

  def commands(env = {}, cwd = @main, flags: {}, config: {}, prompt: csd_prompt(interactive: false))
    settings = csd_settings({ "TMPDIR" => @root, "HOME" => "/Users/me" }.merge(env), cwd,
                            { "machine" => "test-machine", "ready_check" => CHECK }.merge(config), flags)
    Csd::Commands.new(@fake, @waiter, settings, prompt)
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
    refute File.exist?(wt.status_path), "cleared once up is done"
    refute(@fake.ssh_scripts.any? { |script| script.include?("setup-complete") }, "no dotfiles wait without -s")
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

    _, err = capture_io { commands(config: { "ready_check" => "none" }).up("me/feature") }

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
    refute File.exist?(wt.status_path)
  end

  def test_up_passes_settings_to_create
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io do
      commands(flags: { "machine" => "big-machine", "default_permissions" => "true" }).up("me/feature")
    end

    cmd = @fake.calls_matching("gh", "codespace", "create").first
    assert_includes cmd, "big-machine"
    assert_includes cmd, "--default-permissions"
  end

  # --- init ---

  def test_init_writes_a_template_that_parses_to_no_settings
    @fake.passthrough("mkdir")
    path = "#{@root}/config/gh-csd/config.yml"

    _, err = capture_io { commands({ "GH_CSD_CONFIG" => path }).init }

    assert_includes err, "wrote #{path}"
    assert_equal Csd::Config::TEMPLATE, File.read(path)
    assert_equal({}, Csd::Config.parse(File.read(path), path, "me/my-app"))
  end

  def test_init_never_replaces_an_existing_config
    path = "#{@root}/config.yml"
    File.write(path, "machine: mine\n")

    error = assert_raises(Csd::Error) { commands({ "GH_CSD_CONFIG" => path }).init }
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

    error = assert_raises(Csd::Error) { commands({}, wt.path, flags: { "codespace" => "cs-taken" }).run([]) }

    assert_match "in use by other-mac:/src/github-other", error.message
    assert_nil wt.codespace
    assert_empty @fake.calls_matching("rsync")
  end

  def test_run_takeover_claims_the_codespace
    wt = tree("me/feature").tap(&:create)
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("cat ", out: "other-mac:/src/github-other\n")

    capture_io { commands({}, wt.path, flags: { "codespace" => "cs-taken", "takeover" => "true" }).run([]) }

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

  def test_down_without_a_branch_asks_then_deletes_the_worktree_it_runs_in
    wt = tree("me/feature").tap(&:create)

    capture_io { commands({}, wt.path, prompt: csd_prompt("y\n")).down("") }

    refute wt.exist?
  end

  def test_down_without_a_branch_stops_on_no
    wt = tree("me/feature").tap(&:create)

    error = assert_raises(Csd::Error) { commands({}, wt.path, prompt: csd_prompt("\n")).down("") }
    assert_match "cancelled", error.message
    assert wt.exist?
  end

  def test_down_without_a_branch_needs_a_terminal
    wt = tree("me/feature").tap(&:create)

    error = assert_raises(Csd::Error) { commands({}, wt.path).down("") }
    assert_match "pass the branch", error.message
    assert wt.exist?
  end

  # --- up without a branch ---

  def test_up_without_a_branch_uses_the_worktree_it_runs_in
    elsewhere = "#{@root}/elsewhere/me-feature"
    sh("git", "-C", @main, "worktree", "add", "-q", "-b", "me/feature", elsewhere, "origin/master")
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)

    _, err = capture_io { commands({}, elsewhere).up("") }

    assert_includes err, "reusing worktree #{elsewhere}"
    assert_includes @fake.calls_matching("gh", "codespace", "create").first, "me/feature"
    assert_equal "cs-new", Csd::Worktree.current(Csd::Runner.new, elsewhere, "master").codespace
  end

  def test_up_without_a_branch_refuses_the_main_checkout
    error = assert_raises(Csd::Error) { commands.up("") }
    assert_match "main checkout", error.message
  end

  # --- machine ---

  def test_up_picks_the_only_machine_type_without_asking
    @fake.on("gh", "api", out: "only-one\n").on("gh", "codespace", "create", out: "cs-new\n")
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io { commands(config: { "machine" => "" }).up("me/feature") }

    assert_includes @fake.calls_matching("gh", "codespace", "create").first, "only-one"
  end

  def test_up_asks_for_the_machine_type_in_a_terminal
    @fake.on("gh", "api", out: "small\nbig\n").on("gh", "codespace", "create", out: "cs-new\n")
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)

    capture_io { commands(config: { "machine" => "" }, prompt: csd_prompt("2\n")).up("me/feature") }

    assert_includes @fake.calls_matching("gh", "codespace", "create").first, "big"
  end

  def test_up_without_a_terminal_names_the_machine_types
    @fake.on("gh", "api", out: "small\nbig\n")

    error = assert_raises(Csd::Error) { capture_io { commands(config: { "machine" => "" }).up("me/feature") } }

    assert_equal "pass -m with one of: small, big", error.message
    assert_empty @fake.calls_matching("gh", "codespace", "create")
  end

  # --- -s ---

  def test_up_status_waits_for_the_dotfiles_to_finish
    @fake.on("gh", "codespace", "create", out: "cs-new\n").on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on_ssh("setup-complete", status: 1)

    _, err = capture_io { commands(config: { "ready_check" => "none" }).up("me/feature", status: true) }

    assert_includes err, "waiting for the dotfiles to finish..."
    assert_includes err, "dotfiles finished"
    assert_equal 30, @clock.now, "polls again after the first check fails"
    refute File.exist?(tree("me/feature").status_path)
  end

  # --- status ---

  def test_status_lists_each_worktree_with_its_codespace_and_state
    one = tree("me/one").tap(&:create)
    one.bind("cs-one")
    tree("me/two").create
    @fake.on("gh", "codespace", "list", out: "cs-one Available\n")

    out, = capture_io { commands.status }

    assert_equal "me/one\tcs-one\tAvailable\t\t#{one.path}\nme/two\t\t\t\t#{@main}-me-two\n", out
  end

  def test_status_json_prints_the_chosen_fields
    tree("me/one").tap(&:create).bind("cs-gone")
    @fake.on("gh", "codespace", "list", out: "")

    out, = capture_io { commands.status("branch,base,state") }

    assert_equal [{ "branch" => "me/one", "base" => "master", "state" => "Deleted" }], JSON.parse(out)
  end

  def test_status_json_rejects_unknown_fields
    @fake.on("gh", "codespace", "list", out: "")

    error = assert_raises(Csd::UsageError) { commands.status("branch,nope") }
    assert_match "unknown JSON field nope", error.message
  end
end
