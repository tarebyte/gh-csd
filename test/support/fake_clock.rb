# A Clock that advances only when Waiter pauses, so waits finish instantly.
class FakeClock < Csd::Clock
  attr_reader :pauses

  def initialize
    @now = 0.0
    @pauses = []
  end

  def now
    @now
  end

  def pause(seconds)
    @pauses << seconds
    @now += seconds
  end
end
