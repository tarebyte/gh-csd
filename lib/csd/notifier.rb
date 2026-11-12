module Csd
  # Opens pages for the person, such as the permissions approval.
  class Notifier
    #: (Runner) -> void
    def initialize(runner)
      @runner = runner
    end

    #: (String) -> Result
    def open(url)
      @runner.capture(["open", url])
    end
  end
end
