require_relative "test_helper"

# Exercises the real Runner against sh, so a change to how commands run is
# caught on CRuby. spinel/smoke.rb covers the same calls under Spinel.
class RunnerTest < Minitest::Test
  def setup
    @runner = Csd::Runner.new
  end

  def test_capture_separates_streams_and_status
    result = @runner.capture(["sh", "-c", "echo out; echo err >&2; exit 3"])

    assert_equal "out\n", result.out
    assert_equal "err\n", result.err
    assert_equal 3, result.status
    refute result.success?
  end

  def test_capture_passes_stdin
    assert_equal "ABC", @runner.capture(["tr", "a-z", "A-Z"], stdin: "abc").out
  end

  def test_run_returns_the_exit_status
    assert_equal 0, @runner.run(["sh", "-c", "exit 0"])
    assert_equal 4, @runner.run(["sh", "-c", "exit 4"])
  end
end

class FakeRunnerTest < Minitest::Test
  def test_scripted_replies_are_used_once_in_order
    fake = FakeRunner.new.on("gh", "cs", "create", out: "first\n").on("gh", "cs", "create", status: 1)

    assert_equal "first\n", fake.capture(%w[gh cs create -R me/my-app]).out
    assert_equal 1, fake.capture(%w[gh cs create]).status
    assert fake.capture(%w[gh cs create]).success?
    assert_equal 3, fake.calls_matching("gh", "cs", "create").size
  end
end
