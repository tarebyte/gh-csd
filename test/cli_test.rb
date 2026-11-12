require_relative "test_helper"

class CliTest < Minitest::Test
  def cli(*args)
    Csd::Cli.new(args)
  end

  def test_no_command_or_help_shows_the_overview
    [cli, cli("help"), cli("--help"), cli("-h")].each do |parsed|
      assert parsed.help?
      assert_empty parsed.error
      assert_includes parsed.help_text, "COMMANDS"
    end
  end

  def test_command_help_shows_that_commands_help
    parsed = cli("up", "--help")

    assert parsed.help?
    assert_includes parsed.help_text, "gh csd up [<branch>] [flags]"
  end

  def test_unknown_command_and_flag_are_errors
    assert_equal "unknown command nope", cli("nope").error
    assert_equal "unknown flag --nope for up", cli("up", "--nope").error
  end

  def test_up_flags_short_long_and_equals
    parsed = cli("up", "-m", "small", "--devcontainer-path=.devcontainer/x.json", "--default-permissions", "-s", "me/feature")

    assert_equal({ "machine" => "small", "devcontainer" => ".devcontainer/x.json",
                   "default_permissions" => "true", "status" => "true" }, parsed.flags)
    assert_equal ["me/feature"], parsed.words
    assert parsed.flag?("status")
  end

  def test_a_flag_without_its_value_is_an_error
    assert_equal "-m needs a value", cli("up", "-m").error
    assert_equal "-s takes no value", cli("up", "-s=yes").error
  end

  def test_run_takes_flags_only_before_the_command
    parsed = cli("run", "-c", "cs1", "--takeover", "bin/test", "-v", "--seed", "1")

    assert_equal({ "codespace" => "cs1", "takeover" => "true" }, parsed.flags)
    assert_equal ["bin/test", "-v", "--seed", "1"], parsed.words
  end

  def test_double_dash_ends_the_flags
    assert_equal ["--help"], cli("run", "--", "--help").words
    refute cli("run", "--", "--help").help?
  end

  def test_status_json_and_down_force
    assert_equal "branch,state", cli("status", "--json", "branch,state").flags["json"]
    assert cli("down", "-f", "me/x").flag?("force")
  end
end
