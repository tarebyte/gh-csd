module Csd
  # Asks the person at the terminal. Callers check interactive? first and
  # fail with the flag to pass instead, as gh does without a terminal.
  class Prompt
    #: (IO, IO, bool) -> void
    def initialize(input, output, interactive)
      @input = input
      @output = output
      @interactive = interactive
    end

    #: () -> bool
    def interactive?
      @interactive
    end

    # One of options, picked by its number.
    #: (String, Array[String]) -> String
    def choose(question, options)
      @output.puts question
      options.each_with_index { |option, i| @output.puts "  #{i + 1}. #{option}" }
      @output.print "Choose 1-#{options.size}: "
      pick = @input.gets.to_s.strip.to_i
      raise Error, "no choice made" unless pick >= 1 && pick <= options.size

      options[pick - 1].to_s
    end

    #: (String) -> bool
    def confirm?(question)
      @output.print "#{question} [y/N] "
      %w[y yes].include?(@input.gets.to_s.strip.downcase)
    end
  end
end
