require_relative "test_helper"

class WaiterTest < Minitest::Test
  def setup
    @clock = FakeClock.new
    @waiter = Csd::Waiter.new(@clock)
  end

  def test_returns_true_as_soon_as_the_check_passes
    results = [false, false, true]

    assert @waiter.wait(60, 15) { results.shift }
    assert_equal [15, 15], @clock.pauses
  end

  def test_returns_true_without_sleeping_when_already_done
    assert @waiter.wait(60, 15) { true }
    assert_empty @clock.pauses
  end

  def test_gives_up_at_the_deadline_after_a_final_check
    checks = 0

    refute @waiter.wait(60, 15) { checks += 1; false }
    assert_equal 5, checks # at 0, 15, 30, 45, and 60
    assert_equal 60, @clock.now
  end
end

class RemoteFileTest < Minitest::Test
  CONFIG = "Host cs.fake-cs.master\n"

  def setup
    @tmpdir = Dir.mktmpdir
    @fake = FakeRunner.new
    @remote = Csd::Remote.new(@fake, "fake-cs", csd_settings({ "TMPDIR" => @tmpdir, "HOME" => "/Users/me" }))
  end

  def teardown
    FileUtils.rm_rf(@tmpdir)
  end

  def test_true_when_the_file_exists
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG)

    assert @remote.file?("~/.local/state/gh-csd/setup-ready")
    assert_equal "test -f ~/.local/state/gh-csd/setup-ready", @fake.calls_matching("ssh").last.last
  end

  def test_false_when_ssh_fails
    @fake.on("gh", "codespace", "ssh", "--config", out: CONFIG).on("ssh", status: 255)

    refute @remote.file?("~/ready")
  end

  # Regression: the wait loop in `up` gave up on the first SSH failure while
  # the codespace was still starting.
  def test_false_while_the_codespace_cannot_even_give_a_config
    @fake.on("gh", "codespace", "ssh", "--config", err: "codespace is starting", status: 1)

    refute @remote.file?("~/ready")
  end

  def test_a_wait_loop_keeps_polling_through_startup_failures
    @fake.on("gh", "codespace", "ssh", "--config", status: 1)
         .on("gh", "codespace", "ssh", "--config", out: CONFIG)
         .on("ssh", status: 255)
    clock = FakeClock.new
    waiter = Csd::Waiter.new(clock)

    assert waiter.wait(1200, 15) { @remote.file?("~/ready") }
    assert_equal 30, clock.now
  end
end
