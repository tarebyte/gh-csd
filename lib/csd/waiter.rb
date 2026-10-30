module Csd
  # Tests replace it with a clock that advances instantly.
  class Clock
    #: () -> Float
    def now
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    #: (Integer) -> void
    def pause(seconds)
      sleep(seconds)
    end
  end

  class Waiter
    #: (Clock) -> void
    def initialize(clock)
      @clock = clock
    end

    # The block runs at least once, and once more at the deadline.
    #: (Integer, Integer) -> bool
    def wait(timeout, interval)
      deadline = @clock.now + timeout
      loop do
        return true if yield
        return false if @clock.now >= deadline

        @clock.pause(interval)
      end
    end
  end
end
