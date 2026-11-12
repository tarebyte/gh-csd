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

    @codespace.create("me/feature", csd_settings({}, "/", { "machine" => "m" }, { "default_permissions" => "true" }))

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
      assert_match "--default-permissions", error.message
    end
    assert_equal 600, @clock.now
  end

  def test_other_create_failures_raise_with_gh_error
    @fake.on("gh", "codespace", "create", err: "there is no such machine for the repository\n", status: 1)

    error = assert_raises(Csd::Error) { @codespace.create("me/feature", csd_settings({}, "/", {}, { "machine" => "small" })) }
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
  def test_open_opens_the_url
    fake = FakeRunner.new
    Csd::Notifier.new(fake).open("https://example.com/x")

    assert_equal [["open", "https://example.com/x"]], fake.calls
  end
end

class MachinesAndStatesTest < Minitest::Test
  def setup
    @fake = FakeRunner.new
    @codespace = Csd::Codespace.new(@fake, Csd::Notifier.new(@fake), Csd::Waiter.new(FakeClock.new))
  end

  def test_machines_lists_the_repos_machine_types
    @fake.on("gh", "api", out: "small\n\nbig\n")

    assert_equal %w[small big], @codespace.machines(csd_settings({}))
    assert_equal ["gh", "api", "repos/me/my-app/codespaces/machines", "--jq", ".machines[].name"], @fake.calls.first
  end

  def test_states_maps_names_to_states
    @fake.on("gh", "codespace", "list", out: "cs-a Available\ncs-b Shutdown\n")

    assert_equal({ "cs-a" => "Available", "cs-b" => "Shutdown" }, @codespace.states(csd_settings({})))
    assert_includes @fake.calls.first, "me/my-app"
  end
end
