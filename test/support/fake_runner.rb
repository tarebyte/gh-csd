# Stands in for Csd::Runner in tests: records every command and replies with
# scripted results. Unscripted commands succeed with empty output, except
# passthrough commands, which run for real.
class FakeRunner
  attr_reader :calls, :stdins

  def initialize
    @calls = []
    @stdins = []
    @replies = []
    @passthrough = []
    @real = Csd::Runner.new
  end

  # Scripts the reply to the next command whose leading words match prefix.
  # Replies are used once each, in the order they were added.
  def on(*prefix, out: "", err: "", status: 0)
    reply(->(cmd) { cmd.first(prefix.size) == prefix }, out, err, status)
  end

  # Scripts the reply to the next ssh call whose remote script contains
  # fragment.
  def on_ssh(fragment, out: "", err: "", status: 0)
    reply(->(cmd) { cmd.first == "ssh" && cmd.last.include?(fragment) }, out, err, status)
  end

  # Runs commands starting with program (such as "git") for real.
  def passthrough(program)
    @passthrough << program
    self
  end

  def capture(cmd, stdin: "")
    @calls << cmd
    @stdins << stdin
    return @real.capture(cmd, stdin: stdin) if @passthrough.include?(cmd.first)

    reply_for(cmd)
  end

  def run(cmd)
    @calls << cmd
    @stdins << ""
    reply_for(cmd).status
  end

  def spawn_detached(cmd)
    @calls << cmd
    @stdins << ""
    4242
  end

  # What the first recorded command matching the block received on stdin.
  def stdin_of
    index = @calls.index { |cmd| yield cmd }
    index && @stdins[index]
  end

  # The recorded commands whose leading words match prefix.
  def calls_matching(*prefix)
    @calls.select { |cmd| cmd.first(prefix.size) == prefix }
  end

  # The remote scripts of recorded ssh calls.
  def ssh_scripts
    calls_matching("ssh").map(&:last)
  end

  private

  def reply(matcher, out, err, status)
    @replies << [matcher, Csd::Result.new(out, err, status)]
    self
  end

  def reply_for(cmd)
    index = @replies.index { |matcher, _| matcher.call(cmd) }
    index ? @replies.delete_at(index).last : Csd::Result.new("", "", 0)
  end
end
