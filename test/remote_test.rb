require_relative "test_helper"
require "tmpdir"

class RemoteTest < Minitest::Test
  CONFIG = "Host cs.fake-cs.master\n\tUser vscode\n\tProxyCommand gh cs ssh -c fake-cs --stdio\n"

  def setup
    @tmpdir = Dir.mktmpdir
    @fake = FakeRunner.new
    @remote = Csd::Remote.new(@fake, "fake-cs", csd_settings({ "TMPDIR" => @tmpdir, "HOME" => "/Users/me" }))
  end

  def teardown
    FileUtils.rm_rf(@tmpdir)
  end

  def test_config_is_fetched_once_and_cached_across_instances
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    assert_equal "cs.fake-cs.master", @remote.host
    assert_equal "cs.fake-cs.master", Csd::Remote.new(@fake, "fake-cs", csd_settings({ "TMPDIR" => @tmpdir, "HOME" => "/h" })).host
    assert_equal 1, @fake.calls_matching("gh", "codespace", "ssh", "--config").size
  end

  def test_a_failed_config_fetch_raises_and_caches_nothing
    @fake.on("gh", "codespace", "ssh", "--config", err: "no such codespace\n", status: 1)

    error = assert_raises(Csd::Error) { @remote.host }
    assert_match "no such codespace", error.message
    refute File.exist?(File.join(@tmpdir, "gh-csd-fake-cs.ssh"))
  end

  def test_a_still_starting_codespace_is_not_ready_yet
    @fake.on("gh", "codespace", "ssh", "--config", err: "codespace is starting\n", status: 1)

    refute @remote.file?("~/ready")
  end

  def test_a_codespace_without_an_ssh_server_raises_instead_of_waiting
    @fake.on("gh", "codespace", "ssh", "--config", err: "error getting ssh server details: failed to start SSH server: \n", status: 1)

    error = assert_raises(Csd::NoSshServer) { @remote.file?("~/ready") }
    assert_match "features/sshd", error.message
  end

  # Regression: without IdentityAgent=none, every new connection waited
  # about 60s on the 1Password agent.
  def test_every_ssh_call_skips_the_agent_and_shares_a_connection
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    @remote.capture("true")
    @remote.run(%w[true])
    @remote.copy(%w[a.rb], "/src")

    ssh_calls = @fake.calls_matching("ssh")
    assert_equal 2, ssh_calls.size
    ssh_calls.each do |cmd|
      assert_includes cmd, "IdentityAgent=none"
      assert_includes cmd, "ControlMaster=auto"
      assert_includes cmd, "ControlPath=/Users/me/.ssh/gh-csd-%C"
    end
    rsync_shell = @fake.calls_matching("rsync").first[@fake.calls_matching("rsync").first.index("-e") + 1]
    assert_includes rsync_shell, "IdentityAgent=none"
  end

  def test_run_keeps_each_argument_one_word_in_the_remote_shell
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    @remote.run(["bin/test", "test/a b.rb", "x;rm -rf /"])

    script = @fake.calls_matching("ssh").first.last
    assert_equal "cd /workspaces/my-app && bin/test test/a\\ b.rb x\\;rm\\ -rf\\ /", script
    assert_includes @fake.calls_matching("ssh").first, "-t"
  end

  def test_copy_sends_a_nul_separated_list_on_stdin
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
    recorded = nil
    @fake.define_singleton_method(:capture) do |cmd, stdin: ""|
      recorded = stdin if cmd.first == "rsync"
      super(cmd, stdin: stdin)
    end

    @remote.copy(["a.rb", "dir/b c.rb"], "/src")

    assert_equal "a.rb\0dir/b c.rb\0", recorded
    assert_equal ["/src/", "cs.fake-cs.master:/workspaces/my-app/"], @fake.calls_matching("rsync").first.last(2)
  end

  def test_copy_with_no_paths_runs_nothing
    assert @remote.copy([], "/src").success?
    assert_empty @fake.calls
  end

  def test_rsync_shell_is_plain_words_and_refuses_whitespace
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)
    assert_match(/\Assh -F \S+ -o IdentityAgent=none -o ControlMaster=auto -o ControlPath=\/Users\/me\/.ssh\/gh-csd-%C -o ControlPersist=15m\z/, @remote.rsync_shell)

    spaced = Csd::Remote.new(@fake, "fake-cs", csd_settings({ "TMPDIR" => @tmpdir, "HOME" => "/Users/my name" }))
    assert_raises(Csd::Error) { spaced.rsync_shell }
  end
end
