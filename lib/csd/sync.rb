require "shellwords"

module Csd
  # Makes a codespace's checkout match a worktree's changes.
  class Sync
    # Kept from the shell version so its codespaces stay compatible.
    OWNER = ".git/cs-dev-owner"
    RESET = "xargs -0 sh -c 'for p; do " \
            "if git cat-file -e HEAD:\"$p\" 2>/dev/null; then git checkout -q HEAD -- \"$p\"; else rm -f -- \"$p\"; fi; " \
            "done' _"

    #: (Runner, Remote, bool) -> void
    def initialize(runner, remote, takeover)
      @runner = runner
      @remote = remote
      @takeover = takeover
    end

    #: (Worktree) -> void
    def call(tree)
      claim(tree)
      set = change_set(tree)
      apply(set, tree.path)
      warn "gh csd: copied #{set.sync.size}, deleted #{set.gone.size}, reset #{set.reset.size}"
    end

    private

    # One codespace serves one worktree: the reset step would undo another
    # worktree's synced files. The binding is saved only once the claim
    # succeeds.
    #: (Worktree) -> void
    def claim(tree)
      me = "#{@runner.capture(["hostname", "-s"]).out.strip}:#{tree.path}"
      refuse_if_taken(@remote.capture("cat #{owner_file} 2>/dev/null").out.strip, me)
      check(@remote.capture("echo #{Shellwords.escape(me)} > #{owner_file}"), "claim")
      tree.bind(@remote.codespace)
    end

    #: (String, String) -> void
    def refuse_if_taken(owner, me)
      return if owner.empty? || owner == me || @takeover

      raise Error, "#{@remote.codespace} is in use by #{owner}; use another codespace, or pass --takeover"
    end

    #: (Worktree) -> ChangeSet
    def change_set(tree)
      changed = tree.changed_paths
      ChangeSet.new(changed, tree.existing(changed), remote_dirty)
    end

    #: () -> Array[String]
    def remote_dirty
      status = @remote.capture("cd #{dir} && git status --porcelain -z --no-renames --untracked-files=all")
      check(status, "git status")
      ChangeSet.parse_porcelain(status.out)
    end

    #: (ChangeSet, String) -> void
    def apply(set, from_dir)
      check(@remote.copy(set.sync, from_dir), "rsync")
      pipe_paths("xargs -0 rm -f --", set.gone, "delete")
      pipe_paths(RESET, set.reset, "reset")
    end

    #: (String, Array[String], String) -> void
    def pipe_paths(script, paths, step)
      return if paths.empty?

      check(@remote.capture("cd #{dir} && #{script}", stdin: paths.join("\0") + "\0"), step)
    end

    #: (Result, String) -> void
    def check(result, step)
      raise Error, "#{step} failed in the codespace: #{result.err.strip}" unless result.success?
    end

    #: () -> String
    def dir
      Shellwords.escape(@remote.dir)
    end

    #: () -> String
    def owner_file
      "#{dir}/#{OWNER}"
    end
  end
end
