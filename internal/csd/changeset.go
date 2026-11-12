package csd

import (
	"sort"
	"strings"
)

// ChangeSet is which files run copies to, deletes from, and resets in the
// codespace checkout, so that it matches the local worktree's changes.
type ChangeSet struct {
	Sync, Gone, Reset []string
}

// NewChangeSet takes changed (local paths that differ from the merge-base,
// plus untracked), present (the subset that still exists locally), and
// remoteDirty (paths dirty in the codespace checkout).
func NewChangeSet(changed, present, remoteDirty []string) ChangeSet {
	return ChangeSet{
		Sync: sorted(present),
		Gone: sorted(minus(changed, present)),
		// Dirty in the codespace but no longer changed locally: a local edit
		// was reverted, so the codespace goes back to its HEAD for that path.
		Reset: sorted(minus(remoteDirty, changed)),
	}
}

// SplitZ splits NUL-separated paths, dropping empties and duplicates.
func SplitZ(text string) []string {
	var paths []string
	for _, p := range strings.Split(text, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return uniq(paths)
}

// ParsePorcelain reads `git status --porcelain -z --no-renames`, where each
// entry is two status letters, a space, and the path.
func ParsePorcelain(text string) []string {
	var paths []string
	for _, entry := range SplitZ(text) {
		if len(entry) > 3 {
			paths = append(paths, entry[3:])
		}
	}
	return uniq(paths)
}

func uniq(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func minus(items, remove []string) []string {
	drop := map[string]bool{}
	for _, s := range remove {
		drop[s] = true
	}
	var out []string
	for _, s := range items {
		if !drop[s] {
			out = append(out, s)
		}
	}
	return out
}

func sorted(items []string) []string {
	out := append([]string{}, items...)
	sort.Strings(out)
	return out
}
