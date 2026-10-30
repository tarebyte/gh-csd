require_relative "../lib/csd"

USAGE = <<~TEXT
  usage: gh csd <command> [args]

  Edit a GitHub repo in a local worktree; run it in that worktree's own Codespace.

    up <branch>             create or reuse a worktree and its Codespace
    run [command...]        sync the worktree's changes, then run command in the Codespace
    down [--force] <branch> delete the Codespace, worktree, and branch
    init                    write a commented config file to fill in

  Config: ~/.config/gh-csd/config.yml ($XDG_CONFIG_HOME/gh-csd/config.yml, or
  $GH_CSD_CONFIG). Keys, at the top level or under repos: <owner/name>:
  machine, devcontainer, default_permissions, dir, branch, ready_check.

  Environment overrides: CS_MACHINE, CS_DEVCONTAINER, CS_DEFAULT_PERMISSIONS,
  REMOTE_DIR, CS_READY_CHECK ("none" skips the services wait), plus CS (the
  codespace for run) and CS_TAKEOVER.
TEXT

def usage_error
  warn USAGE
  exit 2
end

env = {}
Csd::Settings::KEYS.each { |key| env[key] = ENV[key].to_s }
runner = Csd::Runner.new
settings = Csd::Settings.for_checkout(env, Dir.pwd, File.expand_path($0), runner)
commands = Csd::Commands.new(runner, Csd::Waiter.new(Csd::Clock.new), settings)

args = ARGV.dup
command = args.shift
status = 0
begin
  case command
  when "up"
    usage_error unless args.size == 1
    commands.up(args[0])
  when "run"
    status = commands.run(args)
  when "down"
    force = args.delete("--force") ? true : false
    usage_error unless args.size == 1
    commands.down(args[0], force: force)
  when "init"
    usage_error unless args.empty?
    commands.init
  when "__watch"
    commands.watch(args[0], args[1].to_s, args[2].to_s)
  when nil, "help", "-h", "--help"
    puts USAGE
  else
    warn "gh csd: unknown command #{command}"
    usage_error
  end
rescue Csd::Error => e
  warn "gh csd: #{e.message}"
  status = 1
end
exit status
