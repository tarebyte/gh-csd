module Csd
  # Decides which files `run` copies to, deletes from, and resets in the
  # codespace checkout, so that it matches the local worktree's changes.
  class ChangeSet
    attr_reader :sync, :gone, :reset #: Array[String]

    # changed: local paths that differ from merge-base(HEAD, origin/master),
    #          plus untracked files.
    # present: the subset of changed that still exists locally.
    # remote_dirty: paths that are dirty in the codespace checkout.
    #: (Array[String], Array[String], Array[String]) -> void
    def initialize(changed, present, remote_dirty)
      @sync = present.sort
      @gone = (changed - present).sort
      # Dirty in the codespace but no longer changed locally: a local edit
      # was reverted, so the codespace goes back to its HEAD for that path.
      @reset = (remote_dirty - changed).sort
    end

    #: (String) -> Array[String]
    def self.split_z(text)
      text.split("\0").reject { |path| path.empty? }.uniq
    end

    # Paths from `git status --porcelain -z --no-renames`, where each entry
    # is two status letters, a space, and the path.
    #: (String) -> Array[String]
    def self.parse_porcelain(text)
      split_z(text).map { |entry| entry[3, entry.length - 3] }.uniq
    end
  end
end
