require_relative "test_helper"

class CodespaceTest < Minitest::Test
  DC = ".devcontainer/devcontainer.json"
  APPROVAL_ERR = "You must authorize or deny additional permissions requested by this codespace before continuing.\n" \
                 "Open this URL in your browser to review and authorize additional permissions: " \
                 "https://github.com/me/my-app/codespaces/allow_permissions?ref=master&devcontainer_path=#{DC}\n" \
                 "Alternatively, you can run \"create\" with the \"--default-permissions\" option to continue without authorizing additional permissions.\n"

  def setup
    @fake = FakeRunner.new
    @clock = FakeClock.new
    waiter = Csd::Waiter.new(@clock)
    @codespace = Csd::Codespace.new(@fake, Csd::Notifier.new(@fake), waiter)
  end

  def test_create_returns_the_last_line_of_output
    @fake.on("gh", "codespace", "create", out: "  ✓ Codespaces usage for this repository is paid for by github\nme-feature-abc123\n")

    assert_equal "me-feature-abc123", @codespace.create("me/feature", csd_settings({}))
  end

  def test_create_targets_master_with_explicit_options
    @fake.on("gh", "codespace", "create", out: "cs\n")

    @codespace.create("x" * 60, csd_settings({}))

    cmd = @fake.calls_matching("gh", "codespace", "create").first
    assert_equal ["-R", "me/my-app", "-b", "master", "-m", "test-machine", "--devcontainer-path", DC], cmd[3, 8]
    assert_equal "x" * 48, cmd[cmd.index("-d") + 1]
    refute_includes cmd, "--default-permissions"
  end

  def test_default_permissions_opts_out_of_the_approval
    @fake.on("gh", "codespace", "create", out: "cs\n")

    @codespace.create("me/feature", csd_settings({ "CS_DEFAULT_PERMISSIONS" => "1" }))

    assert_includes @fake.calls_matching("gh", "codespace", "create").first, "--default-permissions"
  end

  def test_approval_opens_the_url_waits_and_creates_again
    @fake.on("gh", "codespace", "create", err: APPROVAL_ERR, status: 1)
         .on("gh", "api", out: "false\n")
         .on("gh", "api", out: "true\n")
         .on("gh", "codespace", "create", out: "cs-after-approval\n")

    _, err = capture_io { assert_equal "cs-after-approval", @codespace.create("me/feature", csd_settings({})) }

    url = "https://github.com/me/my-app/codespaces/allow_permissions?ref=master&devcontainer_path=#{DC}"
    assert_equal [["open", url]], @fake.calls_matching("open")
    assert_includes err, url
    assert_equal 2, @fake.calls_matching("gh", "api").size
    assert_equal 5, @clock.now
  end

  def test_no_approval_within_ten_minutes_raises
    @fake.on("gh", "codespace", "create", err: APPROVAL_ERR, status: 1)

    capture_io do
      error = assert_raises(Csd::Error) { @codespace.create("me/feature", csd_settings({})) }
      assert_match "CS_DEFAULT_PERMISSIONS=1", error.message
    end
    assert_equal 600, @clock.now
  end

  def test_other_create_failures_raise_with_gh_error
    @fake.on("gh", "codespace", "create", err: "there is no such machine for the repository\n", status: 1)

    error = assert_raises(Csd::Error) { @codespace.create("me/feature", csd_settings({ "CS_MACHINE" => "small" })) }
    assert_match "no such machine", error.message
  end

  def test_permissions_check_queries_the_devcontainer
    @fake.on("gh", "api", out: "true\n")

    assert @codespace.permissions_accepted?(csd_settings({}))
    assert_includes @fake.calls.last, "devcontainer_path=#{DC}"
  end

  def test_exist_and_delete
    @fake.on("gh", "codespace", "view", status: 1)

    refute @codespace.exist?("gone")
    @codespace.delete("cs")
    assert_equal ["gh", "codespace", "delete", "-c", "cs", "--force"], @fake.calls.last

    @fake.on("gh", "codespace", "delete", err: "not found", status: 1)
    assert_raises(Csd::Error) { @codespace.delete("cs") }
  end
end

class NotifierTest < Minitest::Test
  def setup
    @fake = FakeRunner.new
    @notifier = Csd::Notifier.new(@fake)
  end

  def test_notify_escapes_applescript_strings
    @notifier.notify("gh csd: ready", "say \"hi\" \\ bye")

    assert_equal ["osascript", "-e", "display notification \"say \\\"hi\\\" \\\\ bye\" with title \"gh csd: ready\""], @fake.calls.last
  end

  # iTerm2 posts OSC 9 notifications with its own icon.
  def test_notify_writes_osc9_to_an_iterm_tty_instead_of_osascript
    Tempfile.create("tty") do |tty|
      @notifier.notify("gh csd: ready", "done in cs1\a", tty: tty.path)

      assert_equal "\e]9;gh csd: ready: done in cs1 \a", File.read(tty.path)
      assert_empty @fake.calls_matching("osascript")
    end
  end

  def test_notify_falls_back_to_osascript_when_the_tty_is_gone
    @notifier.notify("gh csd: ready", "done", tty: "/dev/no-such-tty")

    assert_equal 1, @fake.calls_matching("osascript").size
  end

  def test_iterm_tty_finds_an_iterm_ancestor
    @fake.on("ps", "-o", "tty=", out: "ttys003\n")
         .on("ps", "-o", "comm=", out: "gh-csd\n").on("ps", "-o", "ppid=", out: "200\n")
         .on("ps", "-o", "comm=", out: "/Applications/iTerm.app/Contents/MacOS/iTermServer\n")

    assert_equal "/dev/ttys003", @notifier.iterm_tty
  end

  # Regression: run from Claude Code or a background job, gh csd has no tty
  # of its own, so the watcher fell back to osascript.
  def test_iterm_tty_uses_the_nearest_ancestor_with_a_tty
    @fake.on("ps", "-o", "tty=", out: "??\n")
         .on("ps", "-o", "comm=", out: "gh-csd\n").on("ps", "-o", "ppid=", out: "150\n")
         .on("ps", "-o", "tty=", out: "??\n")
         .on("ps", "-o", "comm=", out: "bash\n").on("ps", "-o", "ppid=", out: "200\n")
         .on("ps", "-o", "tty=", out: "ttys003\n")
         .on("ps", "-o", "comm=", out: "claude\n").on("ps", "-o", "ppid=", out: "300\n")
         .on("ps", "-o", "comm=", out: "/Applications/iTerm.app/Contents/MacOS/iTermServer\n")

    assert_equal "/dev/ttys003", @notifier.iterm_tty
    assert_equal 3, @fake.calls_matching("ps", "-o", "tty=").size, "stops asking once a tty is found"
  end

  def test_iterm_tty_is_empty_outside_iterm
    @fake.on("ps", "-o", "tty=", out: "ttys003\n")
         .on("ps", "-o", "comm=", out: "gh-csd\n").on("ps", "-o", "ppid=", out: "200\n")
         .on("ps", "-o", "comm=", out: "/Applications/Ghostty.app/Contents/MacOS/ghostty\n").on("ps", "-o", "ppid=", out: "1\n")

    assert_equal "", @notifier.iterm_tty
  end

  def test_iterm_tty_is_empty_when_no_ancestor_has_a_tty
    @fake.on("ps", "-o", "tty=", out: "??\n")
         .on("ps", "-o", "comm=", out: "gh-csd\n").on("ps", "-o", "ppid=", out: "200\n")
         .on("ps", "-o", "tty=", out: "??\n")
         .on("ps", "-o", "comm=", out: "/Applications/iTerm.app/Contents/MacOS/iTermServer\n")

    assert_equal "", @notifier.iterm_tty
  end
end
