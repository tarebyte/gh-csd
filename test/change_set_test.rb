require_relative "test_helper"

class ChangeSetTest < Minitest::Test
  def test_present_files_are_synced_and_missing_ones_deleted
    set = Csd::ChangeSet.new(%w[b.rb a.rb gone.rb], %w[a.rb b.rb], [])

    assert_equal %w[a.rb b.rb], set.sync
    assert_equal %w[gone.rb], set.gone
  end

  def test_remote_dirt_outside_the_change_set_is_reset
    set = Csd::ChangeSet.new(%w[a.rb], %w[a.rb], %w[a.rb reverted.rb stray.txt])

    assert_equal %w[reverted.rb stray.txt], set.reset
  end

  def test_remote_dirt_inside_the_change_set_is_not_reset
    set = Csd::ChangeSet.new(%w[a.rb gone.rb], %w[a.rb], %w[a.rb gone.rb])

    assert_empty set.reset
  end

  def test_split_z_drops_empty_entries_and_duplicates
    assert_equal ["a.rb", "dir/b c.rb"], Csd::ChangeSet.split_z("a.rb\0dir/b c.rb\0a.rb\0\0")
  end

  def test_parse_porcelain_strips_status_letters
    text = " M app/models/user.rb\0?? new file.rb\0 D old.rb\0"

    assert_equal ["app/models/user.rb", "new file.rb", "old.rb"], Csd::ChangeSet.parse_porcelain(text)
  end
end
