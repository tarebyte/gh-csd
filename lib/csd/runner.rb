require "open3"

module Csd
  class Result
    attr_reader :out, :err #: String
    attr_reader :status #: Integer

    #: (String, String, Integer) -> void
    def initialize(out, err, status)
      @out = out
      @err = err
      @status = status
    end

    #: () -> bool
    def success?
      @status == 0
    end
  end

  # Every external command goes through here, so tests can swap in a fake.
  class Runner
    #: (Array[String], ?stdin: String) -> Result
    def capture(cmd, stdin: "")
      out, err, status = Open3.capture3(*cmd, stdin_data: stdin)
      Result.new(out, err, status.exitstatus)
    end

    # Attached to the terminal, for the user's own command.
    #: (Array[String]) -> Integer
    def run(cmd)
      system(*cmd)
      $?.exitstatus
    end
  end
end
