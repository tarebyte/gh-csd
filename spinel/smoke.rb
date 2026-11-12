# Runs the library through the same calls as the unit tests, printing the
# results. `make spinel-check` compiles it with Spinel and runs
# `spinel diff`, which fails unless the binary prints exactly what CRuby does.
require_relative "../lib/csd"

runner = Csd::Runner.new
result = runner.capture(["sh", "-c", "echo out; echo err >&2; exit 3"])
puts "capture out=#{result.out.inspect} err=#{result.err.inspect} status=#{result.status} success=#{result.success?}"
puts "capture stdin=#{runner.capture(["tr", "a-z", "A-Z"], stdin: "abc").out.inspect}"
puts "run status=#{runner.run(["sh", "-c", "exit 4"])}"

set = Csd::ChangeSet.new(["b.rb", "a.rb", "gone.rb"], ["a.rb", "b.rb"], ["a.rb", "reverted.rb"])
puts "change_set sync=#{set.sync.inspect} gone=#{set.gone.inspect} reset=#{set.reset.inspect}"
puts "split_z=#{Csd::ChangeSet.split_z("a.rb\0dir/b c.rb\0a.rb\0\0").inspect}"
puts "parse_porcelain=#{Csd::ChangeSet.parse_porcelain(" M app/models/user.rb\0?? new file.rb\0").inspect}"

# Remote against a scripted runner, so no codespace is needed.
class ScriptedRunner < Csd::Runner
  attr_reader :calls

  def initialize
    @calls = []
  end

  def capture(cmd, stdin: "")
    @calls << cmd.join(" ") + (stdin.empty? ? "" : " <<#{stdin.inspect}")
    if cmd[0] == "gh"
      Csd::Result.new("Host cs.smoke.master\n\tUser vscode\n", "", 0)
    else
      Csd::Result.new("", "", 0)
    end
  end

  def run(cmd)
    @calls << cmd.join(" ")
    0
  end
end

tmp = "/tmp/gh-csd-smoke"
Dir.mkdir(tmp) unless Dir.exist?(tmp)
File.delete("#{tmp}/gh-csd-smoke.ssh") if File.exist?("#{tmp}/gh-csd-smoke.ssh")
scripted = ScriptedRunner.new
remote = Csd::Remote.new(scripted, "smoke", Csd::Settings.new({ "TMPDIR" => tmp, "HOME" => "/Users/me" }, "/", config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master"))
puts "remote host=#{remote.host}"
remote.run(["bin/test", "test/a b.rb"])
remote.copy(["a.rb", "dir/b c.rb"], "/src")
scripted.calls.each { |call| puts "remote call=#{call.sub(tmp, "TMP")}" }
begin
  Csd::Remote.new(scripted, "smoke", Csd::Settings.new({ "TMPDIR" => tmp, "HOME" => "/Users/my name" }, "/", config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master")).rsync_shell
rescue Csd::Error => e
  puts "remote error=#{e.message.sub(tmp, "TMP")}"
end
File.delete("#{tmp}/gh-csd-smoke.ssh")
Dir.rmdir(tmp)

# Worktree against real git in a throwaway origin and clone. Commits pass
# -c options so the user's signing config never runs.
root = "/tmp/gh-csd-smoke-wt"
system("rm", "-rf", root)
Dir.mkdir(root)
quiet = { out: File::NULL, err: File::NULL }
commit = ["git", "-c", "commit.gpgsign=false", "-c", "user.name=T", "-c", "user.email=t@example.com"]
system("git", "init", "-q", "--bare", "-b", "master", "#{root}/origin.git", **quiet)
system("git", "clone", "-q", "#{root}/origin.git", "#{root}/my-app", **quiet)
system(*(commit + ["-C", "#{root}/my-app", "commit", "-q", "--allow-empty", "-m", "init"]), **quiet)
system("git", "-C", "#{root}/my-app", "push", "-q", "origin", "HEAD:master", **quiet)

real = Csd::Runner.new
tree = Csd::Worktree.for_branch(real, "me/feature", "#{root}/my-app", "master")
puts "worktree path=#{tree.path.sub(root, "ROOT")} created=#{tree.create} again=#{tree.create}"
tree.bind("cs-smoke")
puts "worktree codespace=#{Csd::Worktree.current(real, tree.path, "master").codespace} clean=#{tree.unsaved_work.empty?}"
File.write("#{tree.path}/new.rb", "1")
changed = tree.changed_paths
present = tree.existing(changed)
puts "worktree changed=#{changed.inspect} present=#{present.inspect} dirty=#{tree.unsaved_work.start_with?("uncommitted:")}"
begin
  tree.remove
rescue Csd::Error
  puts "worktree remove refused=true"
end
tree.remove(force: true)
puts "worktree removed=#{!tree.exist?}"
system("rm", "-rf", root)

# Waiter with a fake clock, and Remote#file? through startup failures.
# A Clock that advances only when Waiter pauses.
class SmokeClock < Csd::Clock
  def initialize
    @now = 0.0
  end

  def now
    @now
  end

  def pause(seconds)
    @now += seconds
  end

  def reset
    @now = 0.0
  end
end

clock = SmokeClock.new
waiter = Csd::Waiter.new(clock)
results = [false, false, true]
puts "waiter passed=#{waiter.wait(60, 15) { results.shift }} at=#{clock.now.to_i}"
clock.reset
checks = 0
puts "waiter timed_out=#{!waiter.wait(60, 15) { checks += 1; false }} checks=#{checks} at=#{clock.now.to_i}"

class FlakyRunner < Csd::Runner
  def initialize
    @step = 0
  end

  def capture(cmd, stdin: "")
    @step += 1
    return Csd::Result.new("", "starting", 1) if @step == 1 # no config yet
    return Csd::Result.new("Host cs.flaky.master\n", "", 0) if cmd[0] == "gh"

    Csd::Result.new("", "", @step >= 4 ? 0 : 255) # ssh fails until the 4th call
  end
end

Dir.mkdir(tmp)
flaky = Csd::Remote.new(FlakyRunner.new, "flaky", Csd::Settings.new({ "TMPDIR" => tmp, "HOME" => "/h" }, "/", config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master"))
clock.reset
puts "remote file_after_startup=#{waiter.wait(1200, 15) { flaky.file?("~/ready") }} at=#{clock.now.to_i}"
File.delete("#{tmp}/gh-csd-flaky.ssh")
Dir.rmdir(tmp)

# Codespace create through the approval flow.
class ApprovalRunner < Csd::Runner
  attr_reader :calls

  def initialize
    @calls = []
    @creates = 0
    @checks = 0
  end

  def capture(cmd, stdin: "")
    @calls << cmd.join(" ")
    if cmd[1] == "codespace" && cmd[2] == "create"
      @creates += 1
      return Csd::Result.new("", "You must authorize or deny additional permissions requested by this codespace before continuing.\n" \
        "Open this URL in your browser to review and authorize additional permissions: https://github.com/me/my-app/codespaces/allow_permissions?ref=master&devcontainer_path=.devcontainer/devcontainer.json\n", 1) if @creates == 1
      return Csd::Result.new("  ✓ paid for by github\nsmoke-cs-123\n", "", 0)
    end
    if cmd[1] == "api"
      @checks += 1
      return Csd::Result.new(@checks >= 2 ? "true\n" : "false\n", "", 0)
    end
    Csd::Result.new("", "", 0)
  end
end

approvals = ApprovalRunner.new
clock.reset
codespace = Csd::Codespace.new(approvals, Csd::Notifier.new(approvals), waiter)
puts "codespace created=#{codespace.create("me/" + "x" * 60, Csd::Settings.new({}, "/", config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master"))} waited=#{clock.now.to_i}"
puts "codespace exist=#{codespace.exist?("smoke-cs-123")}"
codespace.delete("smoke-cs-123")
approvals.calls.each { |call| puts "codespace call=#{call}" }

# Commands: up, run, and down against real git, with gh and ssh scripted.
class CommandRunner < Csd::Runner
  attr_reader :calls

  def initialize
    @calls = []
    @real = Csd::Runner.new
  end

  def capture(cmd, stdin: "")
    return @real.capture(cmd, stdin: stdin) if cmd[0] == "git"

    @calls << cmd.join(" ").gsub(/\/\S*gh-csd-smoke-cmd/, "ROOT").sub(/-p \d+\z/, "-p PID") + (stdin.empty? ? "" : " <<#{stdin.inspect}")
    return Csd::Result.new("cs-cmd\n", "", 0) if cmd[1] == "codespace" && cmd[2] == "create"
    return Csd::Result.new("Host cs.cmd.master\n", "", 0) if cmd[1] == "codespace" && cmd[2] == "ssh"
    return Csd::Result.new("smoke\n", "", 0) if cmd[0] == "hostname"
    return Csd::Result.new(" M stray.rb\0", "", 0) if cmd[0] == "ssh" && cmd[-1].include?("git status")

    Csd::Result.new("", "", 0)
  end

  def run(cmd)
    @calls << cmd.join(" ").gsub(/\/\S*gh-csd-smoke-cmd/, "ROOT")
    7
  end

end

root = "/tmp/gh-csd-smoke-cmd"
system("rm", "-rf", root)
Dir.mkdir(root)
system("git", "init", "-q", "--bare", "-b", "master", "#{root}/origin.git", **quiet)
system("git", "clone", "-q", "#{root}/origin.git", "#{root}/my-app", **quiet)
system(*(commit + ["-C", "#{root}/my-app", "commit", "-q", "--allow-empty", "-m", "init"]), **quiet)
system("git", "-C", "#{root}/my-app", "push", "-q", "origin", "HEAD:master", **quiet)

cmd_runner = CommandRunner.new
clock.reset
cmd_waiter = waiter
cmd_env = { "TMPDIR" => root, "HOME" => "/Users/me" }
no_prompt = Csd::Prompt.new($stdin, $stdout, false)
cmds = Csd::Commands.new(cmd_runner, cmd_waiter, Csd::Settings.new(cmd_env, "#{root}/my-app", config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master"), no_prompt)
cmds.up("me/feature")
puts "commands up=done"
wt_path = "#{root}/my-app-me-feature"
File.write("#{wt_path}/new.rb", "1")
puts "commands run=#{Csd::Commands.new(cmd_runner, cmd_waiter, Csd::Settings.new(cmd_env, wt_path, config: { "machine" => "smoke-machine" }, repo: "me/my-app", default_branch: "master"), no_prompt).run(["bin/test"])}"
cmds.status("branch,codespace,state")
cmds.status
begin
  cmds.down("me/feature")
rescue Csd::Error => e
  puts "commands down_refused=#{e.message.lines.first.strip}"
end
cmds.down("me/feature", force: true)
puts "commands down_force=done"
cmd_runner.calls.each { |call| puts "commands call=#{call}" }
system("rm", "-rf", root)

# Config file lookup and Settings precedence.
cfg_dir = "/tmp/gh-csd-smoke-config"
system("rm", "-rf", cfg_dir)
Dir.mkdir(cfg_dir)
puts "config path=#{Csd::Config.path({ "XDG_CONFIG_HOME" => "/xdg", "HOME" => "/h" })} home=#{Csd::Config.path({ "HOME" => "/h" })}"
puts "config missing=#{Csd::Config.load({ "GH_CSD_CONFIG" => "#{cfg_dir}/none.yml" }, "me/my-app").inspect}"
File.write("#{cfg_dir}/c.yml", "machine: top\ndefault_permissions: true\nrepos:\n  me/my-app:\n    machine: repo\n    ready_check: none\n")
from_file = Csd::Settings.new({ "GH_CSD_CONFIG" => "#{cfg_dir}/c.yml" }, "/", repo: "me/my-app", default_branch: "master")
puts "settings machine=#{from_file.machine} ready_check=#{from_file.ready_check.inspect} perms=#{from_file.default_permissions?} branch=#{from_file.branch}"
overridden = Csd::Settings.new({ "GH_CSD_CONFIG" => "#{cfg_dir}/c.yml", "CS_MACHINE" => "env" }, "/", flags: { "machine" => "flag", "takeover" => "true" }, repo: "me/my-app")
puts "settings flag_machine=#{overridden.machine} takeover=#{overridden.takeover?}"
puts "config repo=#{Csd::Config.repo_from("git@github.com:me/my.repo.git")} none=#{Csd::Config.repo_from("git@gitlab.com:me/r.git").inspect}"
puts "settings default_check=#{Csd::Settings.new({}, "/", config: {}).ready_check}"
File.write("#{cfg_dir}/bad.yml", "- a\n- b\n")
begin
  Csd::Config.load({ "GH_CSD_CONFIG" => "#{cfg_dir}/bad.yml" }, "me/my-app")
rescue Csd::Error => e
  puts "config error=#{e.message.sub(cfg_dir, "DIR")}"
end
system("rm", "-rf", cfg_dir)

# Cli: commands, flags, words, help, and errors.
[["up", "-m", "small", "--default-permissions", "me/x"], ["run", "-c", "cs1", "bin/test", "-v"],
 ["run", "--", "--help"], ["status", "--json", "branch"], ["down", "-f"], ["up", "--nope"], ["nope"], ["up", "-h"], []].each do |args|
  parsed = Csd::Cli.new(args)
  puts "cli #{args.join(" ")}: command=#{parsed.command} words=#{parsed.words.join(",")} flags=#{parsed.flags.keys.sort.map { |k| "#{k}=#{parsed.flags[k]}" }.join(",")} help=#{parsed.help?} error=#{parsed.error}"
end
