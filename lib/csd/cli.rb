module Csd
  # Bad arguments: bin/gh-csd.rb prints the command's help and exits 2.
  class UsageError < Error; end

  # gh csd's arguments, split into a command, its words, and its flags.
  # Flags come before the words; run's command starts at its first word,
  # or after "--".
  class Cli
    # flag => setting. A trailing "!" marks a flag that takes no value.
    FLAGS = {
      "up" => { "-m" => "machine", "--machine" => "machine", "--devcontainer-path" => "devcontainer",
                "--default-permissions" => "default_permissions!", "-s" => "status!", "--status" => "status!" },
      "run" => { "-c" => "codespace", "--codespace" => "codespace", "--takeover" => "takeover!" },
      "down" => { "-f" => "force!", "--force" => "force!" },
      "status" => { "--json" => "json" },
      "init" => {}
    }.freeze

    # The fields of status --json, in output order.
    JSON_FIELDS = %w[worktree branch base codespace state phase].freeze

    HELP = {
      "" => <<~TEXT,
        Edit a GitHub repo in a local worktree, and run it in that worktree's own codespace.

        USAGE
          gh csd <command> [flags]

        COMMANDS
          up:      Create or reuse a codespace for a worktree
          run:     Sync the worktree's changes to its codespace, then run a command there
          status:  Show each worktree and its codespace
          down:    Delete a worktree's codespace, the worktree, and its branch
          init:    Write a commented config file

        Settings come from flags, then ~/.config/gh-csd/config.yml, then defaults.
        Use `gh csd <command> --help` for more about a command.
      TEXT
      "up" => <<~TEXT,
        Create or reuse a codespace for a worktree, then sync the worktree to it.

        With no branch, up uses the worktree you are in. With a branch, up uses
        that branch's worktree, or creates one next to the main checkout. The
        codespace checks out the base branch; run copies your changes onto it.

        USAGE
          gh csd up [<branch>] [flags]

        FLAGS
          -m, --machine string          Machine type; asks in a terminal when not set
              --devcontainer-path string  Path to the devcontainer.json file to use
              --default-permissions     Do not ask to approve the devcontainer's extra permissions
          -s, --status                  Wait until the dotfiles finish too

        EXAMPLES
          $ gh csd up
          $ gh csd up me/fix-login -m standardLinux32gb
      TEXT
      "run" => <<~TEXT,
        Copy the worktree's changes to its codespace, then run a command there.
        With no command, run only syncs. The command runs in a non-login shell,
        as with `gh codespace ssh`.

        USAGE
          gh csd run [flags] [--] [<command> [<args>...]]

        FLAGS
          -c, --codespace string  Use this codespace instead of the worktree's own
              --takeover          Claim a codespace that another worktree owns

        EXAMPLES
          $ gh csd run bin/test test/user_test.rb
          $ gh csd run -- bash -lc 'git fetch origin'
      TEXT
      "status" => <<~TEXT,
        Show each worktree of this repo and its codespace.

        USAGE
          gh csd status [flags]

        FLAGS
          --json fields  Output JSON with the specified fields: #{JSON_FIELDS.join(", ")}

        EXAMPLES
          $ gh csd status
          $ gh csd status --json branch,codespace,state
      TEXT
      "down" => <<~TEXT,
        Delete a worktree's codespace, then the worktree and its local branch.
        Refuses when the branch has work that no remote has.

        With no branch, down uses the worktree you are in and asks first.

        USAGE
          gh csd down [<branch>] [flags]

        FLAGS
          -f, --force  Delete even when the branch has uncommitted or unpushed work

        EXAMPLES
          $ gh csd down me/fix-login
      TEXT
      "init" => <<~TEXT
        Write a commented config file. Never replaces an existing one.

        USAGE
          gh csd init
      TEXT
    }.freeze

    attr_reader :command #: String
    attr_reader :words #: Array[String]
    attr_reader :flags #: Hash[String, String]
    # Why the arguments are wrong, or "".
    attr_reader :error #: String

    #: (Array[String]) -> void
    def initialize(args)
      @command = args.empty? ? "" : args[0].to_s
      @words = []
      @flags = {}
      @help = @command == "" || %w[help -h --help].include?(@command)
      @error = @help || FLAGS.key?(@command) ? "" : "unknown command #{@command}"
      begin
        parse(args.drop(1)) if FLAGS.key?(@command)
      rescue UsageError => e
        @error = e.message
      end
    end

    #: () -> bool
    def help?
      @help
    end

    #: () -> String
    def help_text
      HELP[FLAGS.key?(@command) ? @command : ""].to_s
    end

    #: (String) -> bool
    def flag?(name)
      @flags[name].to_s == "true"
    end

    private

    #: (Array[String]) -> void
    def parse(args)
      known = FLAGS[@command] || {}
      i = 0
      while i < args.size
        arg = args[i].to_s
        if arg == "--"
          @words.concat(args.drop(i + 1))
          return
        elsif arg == "-h" || arg == "--help"
          @help = true
        elsif arg.start_with?("-") && @words.empty?
          i = take_flag(known, args, i)
        elsif @command == "run"
          @words.concat(args.drop(i))
          return
        else
          @words << arg
        end
        i += 1
      end
    end

    # Takes the flag at i, and its value; returns the index of the last
    # argument it used.
    #: (Hash[String, String], Array[String], Integer) -> Integer
    def take_flag(known, args, i)
      name, value = args[i].to_s.split("=", 2)
      setting = known[name.to_s]
      raise UsageError, "unknown flag #{name} for #{@command}" if setting.nil?

      if setting.end_with?("!")
        raise UsageError, "#{name} takes no value" unless value.nil?

        @flags[setting.delete_suffix("!")] = "true"
        return i
      end
      unless value.nil?
        @flags[setting] = value
        return i
      end
      raise UsageError, "#{name} needs a value" if i + 1 >= args.size

      @flags[setting] = args[i + 1].to_s
      i + 1
    end
  end
end
