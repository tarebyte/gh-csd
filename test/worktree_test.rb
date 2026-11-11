require_relative "test_helper"

# Uses real git in a throwaway origin and clone: the behavior that matters
# (worktrees, branches, remotes) is git's own.
class WorktreeTest < Minitest::Test
  include GitRepo

  def setup
    setup_repo
    @runner = Csd::Runner.new
  end

  def teardown
    teardown_repo
  end

  def test_path_sits_next_to_the_main_checkout_with_dashes_for_slashes
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master")

    assert_equal "#{@root}/my-app-me-feature", tree.path
    assert_equal @main, tree.main
  end

  def test_path_is_the_same_from_inside_another_worktree
    first = Csd::Worktree.for_branch(@runner, "me/one", @main, "master")
    first.create

    assert_equal "#{@root}/my-app-me-two", Csd::Worktree.for_branch(@runner, "me/two", first.path, "master").path
  end

  def test_reuses_a_worktree_another_tool_made_for_the_branch
    elsewhere = "#{@root}/elsewhere/my-app/me-feature"
    sh("git", "-C", @main, "worktree", "add", "-q", "-b", "me/feature", elsewhere, "origin/master")
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master")

    assert_equal elsewhere, tree.path
    refute tree.create
    tree.remove
    refute Dir.exist?(elsewhere)
  end

  def test_never_picks_the_main_checkout
    tree = Csd::Worktree.for_branch(@runner, "master", @main, "master")

    assert_equal "#{@root}/my-app-master", tree.path
  end

  def test_create_branches_from_origin_master_and_reuses_an_existing_tree
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master")

    assert tree.create
    assert_equal "me/feature", sh("git", "-C", tree.path, "branch", "--show-current")
    refute tree.create
  end

  def test_create_branches_from_the_given_base
    sh("git", "-C", @main, "commit", "-q", "--allow-empty", "-m", "on main")
    sh("git", "-C", @main, "push", "-q", "origin", "HEAD:main")
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "main")

    assert tree.create
    assert_equal "on main", sh("git", "-C", tree.path, "log", "-1", "--format=%s")
  end

  def test_create_checks_out_an_existing_local_branch
    sh("git", "-C", @main, "branch", "me/existing")
    tree = Csd::Worktree.for_branch(@runner, "me/existing", @main, "master")

    assert tree.create
    assert_equal "me/existing", sh("git", "-C", tree.path, "branch", "--show-current")
  end

  def test_binding_lives_in_the_worktrees_own_git_dir
    one = Csd::Worktree.for_branch(@runner, "me/one", @main, "master").tap(&:create)
    two = Csd::Worktree.for_branch(@runner, "me/two", @main, "master").tap(&:create)

    assert_nil one.codespace
    one.bind("cs-one")
    two.bind("cs-two")

    assert_equal "cs-one", one.codespace
    assert_equal "cs-two", Csd::Worktree.current(@runner, two.path, "master").codespace
    assert File.exist?("#{@main}/.git/worktrees/my-app-me-one/cs-dev-codespace")
  end

  def test_unsaved_work_lists_uncommitted_and_unpushed_work
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master").tap(&:create)
    assert_empty tree.unsaved_work

    File.write("#{tree.path}/new.txt", "x")
    assert_match(/uncommitted:\n\?\? new.txt/, tree.unsaved_work)

    sh("git", "-C", tree.path, "add", "new.txt")
    sh("git", "-C", tree.path, "commit", "-q", "-m", "wip")
    assert_match(/unpushed:\n\h+ wip/, tree.unsaved_work)

    sh("git", "-C", tree.path, "push", "-q", "origin", "me/feature")
    assert_empty tree.unsaved_work
  end

  def test_remove_deletes_the_worktree_and_branch
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master").tap(&:create)

    tree.remove

    refute tree.exist?
    assert_empty sh("git", "-C", @main, "branch", "--list", "me/feature")
  end

  def test_remove_with_force_deletes_a_dirty_worktree
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master").tap(&:create)
    File.write("#{tree.path}/new.txt", "x")

    assert_raises(Csd::Error) { tree.remove }
    tree.remove(force: true)
    refute tree.exist?
  end

  def test_changes_lists_diffs_untracked_and_deleted_paths
    sh("git", "-C", @main, "commit", "-q", "--allow-empty", "-m", "noop")
    File.write("#{@main}/kept.rb", "1")
    File.write("#{@main}/doomed.rb", "1")
    sh("git", "-C", @main, "add", ".")
    sh("git", "-C", @main, "commit", "-q", "-m", "files")
    sh("git", "-C", @main, "push", "-q", "origin", "HEAD:master")
    tree = Csd::Worktree.for_branch(@runner, "me/feature", @main, "master").tap(&:create)

    File.write("#{tree.path}/kept.rb", "2")
    File.delete("#{tree.path}/doomed.rb")
    File.write("#{tree.path}/new file.rb", "3")
    current = Csd::Worktree.current(@runner, tree.path, "master")
    changed = current.changed_paths
    present = current.existing(changed)

    assert_equal ["doomed.rb", "kept.rb", "new file.rb"], changed.sort
    assert_equal ["kept.rb", "new file.rb"], present.sort
  end

  def test_git_failures_raise
    assert_raises(Csd::Error) { Csd::Worktree.current(@runner, @root, "master") }
  end
end
