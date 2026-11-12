require_relative "../lib/csd"

# Parsing, help, and dispatch only; the work is in lib/csd.
cli = Csd::Cli.new(ARGV.dup)
status = 0
begin
  raise Csd::UsageError, cli.error unless cli.error.empty?

  if cli.help?
    puts cli.help_text
    exit 0
  end

  env = {}
  Csd::Settings::KEYS.each { |key| env[key] = ENV[key].to_s }
  runner = Csd::Runner.new
  settings = Csd::Settings.for_checkout(env, Dir.pwd, cli.flags, runner)
  prompt = Csd::Prompt.new($stdin, $stderr, $stdin.tty? && $stderr.tty?)
  commands = Csd::Commands.new(runner, Csd::Waiter.new(Csd::Clock.new), settings, prompt)
  words = cli.words

  case cli.command
  when "up"
    raise Csd::UsageError, "up takes at most one branch" if words.size > 1

    commands.up(words[0].to_s, status: cli.flag?("status"))
  when "run"
    status = commands.run(words)
  when "status"
    raise Csd::UsageError, "status takes no arguments" unless words.empty?

    commands.status(cli.flags["json"].to_s)
  when "down"
    raise Csd::UsageError, "down takes at most one branch" if words.size > 1

    commands.down(words[0].to_s, force: cli.flag?("force"))
  when "init"
    raise Csd::UsageError, "init takes no arguments" unless words.empty?

    commands.init
  end
rescue Csd::UsageError => e
  warn "gh csd: #{e.message}"
  warn ""
  warn cli.help_text
  status = 2
rescue Csd::Error => e
  warn "gh csd: #{e.message}"
  status = 1
end
exit status
