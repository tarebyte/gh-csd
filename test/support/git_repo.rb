require "tmpdir"
require "fileutils"
require "open3"

# A throwaway origin and clone (main checkout at #{@root}/my-app), with the
# user's git config ignored so commit signing and hooks never run.
module GitRepo
  GIT_ENV = {
    "GIT_CONFIG_GLOBAL" => "/dev/null", "GIT_CONFIG_NOSYSTEM" => "1",
    "GIT_AUTHOR_NAME" => "Test", "GIT_AUTHOR_EMAIL" => "test@example.com",
    "GIT_COMMITTER_NAME" => "Test", "GIT_COMMITTER_EMAIL" => "test@example.com"
  }.freeze

  def setup_repo
    isolate_git_config
    @root = File.realpath(Dir.mktmpdir)
    @main = "#{@root}/my-app"
    clone_with_initial_commit
  end

  def teardown_repo
    FileUtils.rm_rf(@root)
    @saved_env.each { |key, value| ENV[key] = value }
  end

  def sh(*cmd)
    out, err, status = Open3.capture3(*cmd)
    raise "#{cmd.join(" ")}: #{err}" unless status.success?

    out.strip
  end

  private

  def isolate_git_config
    @saved_env = GIT_ENV.keys.to_h { |key| [key, ENV[key]] }
    ENV.update(GIT_ENV)
  end

  def clone_with_initial_commit
    sh("git", "init", "-q", "--bare", "-b", "master", "#{@root}/origin.git")
    sh("git", "clone", "-q", "#{@root}/origin.git", @main)
    sh("git", "-C", @main, "commit", "-q", "--allow-empty", "-m", "init")
    sh("git", "-C", @main, "push", "-q", "origin", "HEAD:master")
  end
end
