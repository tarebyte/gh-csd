module Csd
  # A local worktree of the main checkout, one per branch, and the codespace
  # it is bound to.
  class Worktree
    # Read by a status line in the user's dotfiles; keep the name.
    BINDING = "cs-dev-codespace"
    # What gh csd is doing with the codespace, also for the status line.
    STATUS = "cs-dev-status"

    attr_reader :path #: String
    attr_reader :branch, :main #: String?

    # The worktree for branch: the one git already has for it, wherever
    # another tool made it; else next to the main checkout, with slashes in
    # the branch name turned into dashes (my-app -> my-app-me-feature).
    # base is the branch on origin that new worktrees start from.
    #: (Runner, String, String, String) -> Worktree
    def self.for_branch(runner, branch, cwd, base)
      common = git(runner, cwd, ["rev-parse", "--path-format=absolute", "--git-common-dir"])
      main = common.sub(%r{/\.git\z}, "")
      path = existing_path(runner, main, branch)
      path = "#{main}-#{branch.tr("/", "-")}" if path.empty?
      new(runner, path, base, branch, main)
    end

    # The path of branch's linked worktree, or "" when it has none. Never
    # the main checkout: down would try to remove it.
    #: (Runner, String, String) -> String
    def self.existing_path(runner, main, branch)
      found = ""
      git(runner, main, ["worktree", "list", "--porcelain"]).split("\n\n").each do |entry|
        lines = entry.split("\n")
        path = lines[0].to_s.sub(/\Aworktree /, "")
        found = path if path != main && lines.include?("branch refs/heads/#{branch}")
      end
      found
    end

    #: (Runner, String, String) -> Worktree
    def self.current(runner, cwd, base)
      new(runner, git(runner, cwd, ["rev-parse", "--show-toplevel"]), base, nil, nil)
    end

    #: (Runner, String, Array[String]) -> String
    def self.git(runner, dir, args)
      result = runner.capture(["git", "-C", dir] + args)
      raise Error, "git #{args.join(" ")} failed in #{dir}: #{result.err.strip}" unless result.success?

      result.out.strip
    end

    #: (Runner, String, String, String?, String?) -> void
    def initialize(runner, path, base, branch, main)
      @runner = runner
      @path = path
      @base = base
      @branch = branch
      @main = main
    end

    #: () -> bool
    def exist?
      Dir.exist?(@path)
    end

    # True when it created the worktree, false when it already existed.
    #: () -> bool
    def create
      git(@main, ["fetch", "-q", "origin", @base])
      return false if exist?

      add_worktree
      true
    end

    #: () -> String?
    def codespace
      file = binding_path
      return nil unless File.exist?(file)

      name = File.read(file).strip
      name.empty? ? nil : name
    end

    #: (String) -> void
    def bind(codespace)
      File.write(binding_path, "#{codespace}\n")
    end

    #: () -> String
    def status_path
      File.join(git(@path, ["rev-parse", "--absolute-git-dir"]), STATUS)
    end

    # "" clears it. A class method too, for the watcher, which has only the path.
    #: (String) -> void
    def status=(phase)
      Worktree.write_status(status_path, phase)
    end

    #: (String, String) -> void
    def self.write_status(path, phase)
      return File.write(path, "#{phase}\n") unless phase.empty?

      File.delete(path) if File.exist?(path)
    end

    # Work that removing the worktree and branch would lose: uncommitted or
    # untracked files, and commits that no remote has. Empty when safe.
    #: () -> String
    def unsaved_work
      [section("uncommitted", git(@path, ["status", "--porcelain"])),
       section("unpushed", git(@main, ["log", "--oneline", @branch, "--not", "--remotes"]))].compact.join("\n")
    end

    #: (?force: bool) -> void
    def remove(force: false)
      args = ["worktree", "remove"]
      args << "--force" if force
      git(@main, args + [@path])
      git(@main, ["branch", "-D", @branch])
    end

    # Paths that differ from merge-base(HEAD, origin/<base>), plus untracked.
    #: () -> Array[String]
    def changed_paths
      base = git(@path, ["merge-base", "HEAD", "origin/#{@base}"])
      diff = @runner.capture(["git", "-C", @path, "diff", "-z", "--no-renames", "--name-only", base]).out
      untracked = @runner.capture(["git", "-C", @path, "ls-files", "-z", "--others", "--exclude-standard"]).out
      (ChangeSet.split_z(diff) + ChangeSet.split_z(untracked)).uniq
    end

    #: (Array[String]) -> Array[String]
    def existing(paths)
      paths.select { |path| File.exist?(File.join(@path, path)) }
    end

    private

    # An existing local branch, or a new one from origin/<base>.
    #: () -> void
    def add_worktree
      if branch_exists?
        git(@main, ["worktree", "add", "-q", @path, @branch])
      else
        git(@main, ["worktree", "add", "-q", "-b", @branch, @path, "origin/#{@base}"])
      end
    end

    #: () -> bool
    def branch_exists?
      @runner.capture(["git", "-C", @main, "show-ref", "-q", "--verify", "refs/heads/#{@branch}"]).success?
    end

    #: (String, String) -> String?
    def section(label, text)
      text.empty? ? nil : "#{label}:\n#{text}"
    end

    #: () -> String
    def binding_path
      File.join(git(@path, ["rev-parse", "--absolute-git-dir"]), BINDING)
    end

    #: (String, Array[String]) -> String
    def git(dir, args)
      Worktree.git(@runner, dir, args)
    end
  end
end
