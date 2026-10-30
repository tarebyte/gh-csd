module Csd
  class Notifier
    #: (Runner) -> void
    def initialize(runner)
      @runner = runner
    end

    #: (String) -> Result
    def open(url)
      @runner.capture(["open", url])
    end

    # With an iTerm2 tty, iTerm2 posts the notification (OSC 9), so it shows
    # iTerm2's icon; osascript's always shows Script Editor's. Falls back to
    # osascript when the tty is gone or unwritable.
    #: (String, String, ?tty: String) -> void
    def notify(title, message, tty: "")
      return if !tty.empty? && write_osc9(tty, "#{title}: #{message}")

      script = "display notification #{applescript_string(message)} with title #{applescript_string(title)}"
      @runner.capture(["osascript", "-e", script])
    end

    # The tty of the nearest ancestor that has one, when an iTerm2 process is
    # also an ancestor, or "". gh csd itself often has no tty (Claude Code and
    # background jobs run it detached), and TERM_PROGRAM is not passed down.
    #: () -> String
    def iterm_tty
      pid = Process.pid.to_s
      tty = ""
      40.times do
        tty = tty_of(pid) if tty.empty?
        return (tty.empty? ? "" : "/dev/#{tty}") if ps(pid, "comm=").include?("iTerm")

        pid = ps(pid, "ppid=")
        return "" if pid.empty? || pid == "0" || pid == "1"
      end
      ""
    end

    private

    #: (String) -> String
    def tty_of(pid)
      tty = ps(pid, "tty=")
      tty == "??" ? "" : tty
    end

    #: (String, String) -> String
    def ps(pid, field)
      @runner.capture(["ps", "-o", field, "-p", pid]).out.strip
    end

    #: (String, String) -> bool
    def write_osc9(tty, text)
      return false unless File.writable?(tty)

      File.write(tty, "\033]9;#{text.tr("\a\033", "  ")}\a")
      true
    rescue StandardError
      false
    end

    #: (String) -> String
    def applescript_string(text)
      "\"" + text.gsub("\\", "\\\\\\\\").gsub("\"", "\\\\\"") + "\""
    end
  end
end
