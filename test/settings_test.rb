require_relative "test_helper"

class ConfigTest < Minitest::Test
  def setup
    @dir = Dir.mktmpdir
  end

  def teardown
    FileUtils.rm_rf(@dir)
  end

  def parse(yaml)
    Csd::Config.parse(yaml, "c.yml", "me/my-app")
  end

  def test_path_prefers_gh_csd_config_then_xdg_then_home
    assert_equal "/x/my.yml", Csd::Config.path("GH_CSD_CONFIG" => "/x/my.yml", "XDG_CONFIG_HOME" => "/xdg", "HOME" => "/h")
    assert_equal "/xdg/gh-csd/config.yml", Csd::Config.path("XDG_CONFIG_HOME" => "/xdg", "HOME" => "/h")
    assert_equal "/h/.config/gh-csd/config.yml", Csd::Config.path("XDG_CONFIG_HOME" => "", "HOME" => "/h")
  end

  def test_a_missing_file_is_an_empty_config
    assert_equal({}, Csd::Config.load({ "GH_CSD_CONFIG" => "#{@dir}/none.yml" }, "me/my-app"))
  end

  def test_an_empty_file_is_an_empty_config
    assert_equal({}, parse(""))
  end

  def test_the_repo_section_beats_the_top_level
    settings = parse("machine: top\ndir: /top\nrepos:\n  me/my-app:\n    machine: repo\n    ready_check: none\n")

    assert_equal({ "machine" => "repo", "dir" => "/top", "ready_check" => "none" }, settings)
  end

  def test_other_repos_sections_are_ignored
    assert_equal({}, parse("repos:\n  other/repo:\n    machine: other\n"))
  end

  def test_the_section_follows_the_origin_repo
    assert_equal({ "machine" => "other" }, Csd::Config.parse("repos:\n  other/repo:\n    machine: other\n", "c.yml", "other/repo"))
  end

  def test_repo_from_reads_ssh_and_https_origins
    assert_equal "me/my-app", Csd::Config.repo_from("git@github.com:me/my-app.git")
    assert_equal "me/my.repo", Csd::Config.repo_from("https://github.com/me/my.repo.git")
    assert_equal "me/repo", Csd::Config.repo_from("ssh://git@github.com/me/repo")
    assert_equal "", Csd::Config.repo_from("git@gitlab.com:me/repo.git")
    assert_equal "", Csd::Config.repo_from("")
  end

  def test_a_malformed_repos_key_is_ignored
    assert_equal({ "machine" => "top" }, parse("machine: top\nrepos: oops\n"))
  end

  # Scalars become strings, so nothing untyped from YAML travels further.
  def test_values_become_strings_and_non_scalars_are_dropped
    settings = parse("default_permissions: true\nmachine: 123\nlist: [a, b]\nempty:\nnested:\n  a: 1\n")

    assert_equal({ "default_permissions" => "true", "machine" => "123" }, settings)
  end

  def test_load_reads_the_file
    File.write("#{@dir}/c.yml", "machine: from-file\n")

    assert_equal({ "machine" => "from-file" }, Csd::Config.load({ "GH_CSD_CONFIG" => "#{@dir}/c.yml" }, "me/my-app"))
  end

  def test_invalid_yaml_is_a_clear_error
    error = assert_raises(Csd::Error) { parse("machine: [unclosed\n") }
    assert_match "c.yml is not valid YAML", error.message
  end

  def test_a_non_mapping_is_a_clear_error
    error = assert_raises(Csd::Error) { parse("- just\n- a list\n") }
    assert_match "must be a mapping", error.message
  end
end

class SettingsTest < Minitest::Test
  def test_built_in_defaults_name_no_repo
    settings = Csd::Settings.new({}, "/", config: {}, repo: "me/my-app")

    assert_equal ".devcontainer/devcontainer.json", settings.devcontainer
    assert_equal "/workspaces/my-app", settings.remote_dir
    assert_equal "", settings.ready_check
    refute settings.default_permissions?
  end

  def test_machine_has_no_default
    assert_equal "", Csd::Settings.new({}, "/", config: {}).machine
  end

  def test_flag_beats_config_beats_default
    config = { "machine" => "from-config" }

    assert_equal "flag", csd_settings({}, "/", config, { "machine" => "flag" }).machine
    assert_equal "from-config", csd_settings({}, "/", config).machine
    assert_equal "master", csd_settings({}, "/", config).branch
  end

  def test_environment_variables_no_longer_override
    assert_equal "from-config", csd_settings({ "CS_MACHINE" => "env" }, "/", { "machine" => "from-config" }).machine
  end

  def test_a_chosen_machine_beats_the_config
    settings = csd_settings({}, "/", { "machine" => "from-config" })
    settings.machine = "chosen"

    assert_equal "chosen", settings.machine
  end

  def test_ready_check_none_means_no_wait
    assert_equal "", csd_settings({}, "/", { "ready_check" => "none" }).ready_check
    assert_equal "pg_isready", csd_settings({}, "/", { "ready_check" => "pg_isready" }).ready_check
  end

  def test_default_permissions_reads_strings_from_yaml_and_flags
    assert csd_settings({}, "/", { "default_permissions" => "true" }).default_permissions?
    refute csd_settings({}, "/", { "default_permissions" => "false" }).default_permissions?
    assert csd_settings({}, "/", {}, { "default_permissions" => "true" }).default_permissions?
  end

  def test_codespace_and_takeover_come_from_flags
    settings = csd_settings({}, "/", {}, { "codespace" => "cs1", "takeover" => "true" })

    assert_equal "cs1", settings.codespace
    assert settings.takeover?
    refute csd_settings({}).takeover?
  end

  def test_reads_the_config_file_when_not_given_one
    Dir.mktmpdir do |dir|
      File.write("#{dir}/c.yml", "repos:\n  me/my-app:\n    machine: from-file\n")

      assert_equal "from-file", Csd::Settings.new({ "GH_CSD_CONFIG" => "#{dir}/c.yml" }, "/", repo: "me/my-app").machine
    end
  end

  def test_branch_falls_back_to_origins_default_then_main
    assert_equal "trunk", Csd::Settings.new({}, "/", config: {}, default_branch: "trunk").branch
    assert_equal "main", Csd::Settings.new({}, "/", config: {}).branch
    assert_equal "from-config", Csd::Settings.new({}, "/", config: { "branch" => "from-config" }, default_branch: "trunk").branch
  end

  def test_for_checkout_reads_the_origin_remote
    runner = FakeRunner.new
    runner.on("git", "-C", "/src/x", "remote", out: "git@github.com:me/repo.git\n")
    runner.on("git", "-C", "/src/x", "symbolic-ref", out: "origin/trunk\n")
    settings = Csd::Settings.for_checkout({ "GH_CSD_CONFIG" => "/none.yml" }, "/src/x", {}, runner)

    assert_equal "me/repo", settings.repo
    assert_equal "trunk", settings.branch
  end

  def test_repo_without_a_github_origin_is_a_clear_error
    error = assert_raises(Csd::Error) { Csd::Settings.new({}, "/src/x", config: {}).repo }
    assert_match "no github.com origin remote in /src/x", error.message
  end
end
