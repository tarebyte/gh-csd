package csd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// BindingFile names the worktree's codespace. A status line in the
	// user's dotfiles reads it; keep the name.
	BindingFile = "cs-dev-codespace"
	// StatusFile says what up is doing with the codespace.
	StatusFile = "cs-dev-status"
)

// Worktree is a local worktree of the main checkout, one per branch.
type Worktree struct {
	runner Runner
	Path   string
	base   string
	Branch string
	Main   string
}

// ForBranch is branch's worktree: the one git already has, wherever
// another tool made it; else next to the main checkout, with slashes in
// the branch name turned into dashes (my-app -> my-app-me-feature).
func ForBranch(ctx context.Context, r Runner, branch, cwd, base string) (*Worktree, error) {
	main, linked, err := worktreeList(ctx, r, cwd)
	if err != nil {
		return nil, err
	}
	path := main + "-" + strings.ReplaceAll(branch, "/", "-")
	for _, l := range linked {
		if l[1] == branch {
			path = l[0]
		}
	}
	return &Worktree{r, path, base, branch, main}, nil
}

// Here is the linked worktree at cwd, for up and down without a branch.
func Here(ctx context.Context, r Runner, cwd, base string) (*Worktree, error) {
	top, err := git(ctx, r, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	branch, err := git(ctx, r, cwd, "branch", "--show-current")
	if err != nil {
		return nil, err
	}
	if branch == "" {
		return nil, fmt.Errorf("%s has no branch checked out; pass a branch", top)
	}
	tree, err := ForBranch(ctx, r, branch, cwd, base)
	if err != nil {
		return nil, err
	}
	if tree.Path != top {
		return nil, fmt.Errorf("%s is the main checkout; pass a branch, or run from a worktree", top)
	}
	return tree, nil
}

// All is every linked worktree of the checkout at cwd, in git's order.
func All(ctx context.Context, r Runner, cwd, base string) ([]*Worktree, error) {
	main, linked, err := worktreeList(ctx, r, cwd)
	if err != nil {
		return nil, err
	}
	trees := make([]*Worktree, 0, len(linked))
	for _, l := range linked {
		trees = append(trees, &Worktree{r, l[0], base, l[1], main})
	}
	return trees, nil
}

// Current is the worktree at cwd, for run.
func Current(ctx context.Context, r Runner, cwd, base string) (*Worktree, error) {
	top, err := git(ctx, r, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	return &Worktree{runner: r, Path: top, base: base}, nil
}

// worktreeList is the main checkout (git lists it first) and [path, branch]
// of every other worktree; branch is "" when detached. The main checkout is
// never among them: down would try to remove it. -z keeps paths exactly as
// they are, whatever characters they hold.
func worktreeList(ctx context.Context, r Runner, cwd string) (string, [][2]string, error) {
	out, err := gitRaw(ctx, r, cwd, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", nil, err
	}
	var main, path, branch string
	var linked [][2]string
	// Each attribute ends in NUL, and an empty attribute ends a worktree.
	for _, attr := range strings.Split(out, "\x00") {
		switch {
		case attr == "" && path != "":
			if main == "" {
				main = path
			} else {
				linked = append(linked, [2]string{path, branch})
			}
			path, branch = "", ""
		case strings.HasPrefix(attr, "worktree "):
			path = strings.TrimPrefix(attr, "worktree ")
		case strings.HasPrefix(attr, "branch refs/heads/"):
			branch = strings.TrimPrefix(attr, "branch refs/heads/")
		}
	}
	return main, linked, nil
}

// git runs git in dir and returns its output without surrounding space.
func git(ctx context.Context, r Runner, dir string, args ...string) (string, error) {
	out, err := gitRaw(ctx, r, dir, args...)
	return strings.TrimSpace(out), err
}

// gitRaw keeps the output as git wrote it, for NUL-separated paths that
// may start or end with spaces.
func gitRaw(ctx context.Context, r Runner, dir string, args ...string) (string, error) {
	res, err := r.Capture(ctx, append([]string{"git", "-C", dir}, args...), "")
	if err != nil {
		return "", err
	}
	if !res.OK() {
		return "", fmt.Errorf("git %s failed in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(res.Err))
	}
	return res.Out, nil
}

// Exist reports whether the worktree's folder exists.
func (w *Worktree) Exist() bool {
	info, err := os.Stat(w.Path)
	return err == nil && info.IsDir()
}

// Create makes the worktree, and reports whether it was new.
func (w *Worktree) Create(ctx context.Context) (bool, error) {
	if _, err := git(ctx, w.runner, w.Main, "fetch", "-q", "origin", w.base); err != nil {
		return false, err
	}
	if w.Exist() {
		return false, nil
	}
	res, err := w.runner.Capture(ctx, []string{"git", "-C", w.Main, "show-ref", "-q", "--verify", "refs/heads/" + w.Branch}, "")
	if err != nil {
		return false, err
	}
	args := []string{"worktree", "add", "-q", "-b", w.Branch, w.Path, "origin/" + w.base}
	if res.OK() {
		args = []string{"worktree", "add", "-q", w.Path, w.Branch}
	}
	_, err = git(ctx, w.runner, w.Main, args...)
	return err == nil, err
}

// Codespace is the bound codespace's name, or "" when none is bound. Any
// other failure to read the binding is an error: guessing "none" would let
// down orphan a codespace, or up create a second one.
func (w *Worktree) Codespace(ctx context.Context) (string, error) {
	path, err := w.gitFile(ctx, BindingFile)
	if err != nil {
		return "", err
	}
	text, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the codespace binding: %w", err)
	}
	return strings.TrimSpace(string(text)), nil
}

// Bind records the worktree's codespace.
func (w *Worktree) Bind(ctx context.Context, codespace string) error {
	path, err := w.gitFile(ctx, BindingFile)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(codespace+"\n"), 0o644)
}

// Phase is what up is doing with the codespace ("" when nothing). It is
// shown, never relied on, so a failure to read it is "".
func (w *Worktree) Phase(ctx context.Context) string {
	path, err := w.gitFile(ctx, StatusFile)
	if err != nil {
		return ""
	}
	text, _ := os.ReadFile(path)
	return strings.TrimSpace(string(text))
}

// SetPhase records it; "" clears it. It is shown, never relied on, so a
// failure to write it is ignored.
func (w *Worktree) SetPhase(ctx context.Context, phase string) {
	path, err := w.gitFile(ctx, StatusFile)
	if err != nil {
		return
	}
	if phase == "" {
		os.Remove(path)
		return
	}
	os.WriteFile(path, []byte(phase+"\n"), 0o644)
}

// UnsavedWork is what removing the worktree and branch would lose:
// uncommitted or untracked files, and commits no remote has. "" when safe.
func (w *Worktree) UnsavedWork(ctx context.Context) (string, error) {
	dirty, err := git(ctx, w.runner, w.Path, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	unpushed, err := git(ctx, w.runner, w.Main, "log", "--oneline", w.Branch, "--not", "--remotes")
	if err != nil {
		return "", err
	}
	var parts []string
	if dirty != "" {
		parts = append(parts, "uncommitted:\n"+dirty)
	}
	if unpushed != "" {
		parts = append(parts, "unpushed:\n"+unpushed)
	}
	return strings.Join(parts, "\n"), nil
}

// Remove deletes the worktree and its local branch.
func (w *Worktree) Remove(ctx context.Context, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	if _, err := git(ctx, w.runner, w.Main, append(args, w.Path)...); err != nil {
		return err
	}
	_, err := git(ctx, w.runner, w.Main, "branch", "-D", w.Branch)
	return err
}

// ChangedPaths differ from merge-base(HEAD, origin/<base>), plus untracked.
// Any git failure is an error: an incomplete list would make sync reset
// the missing paths in the codespace.
func (w *Worktree) ChangedPaths(ctx context.Context) ([]string, error) {
	base, err := git(ctx, w.runner, w.Path, "merge-base", "HEAD", "origin/"+w.base)
	if err != nil {
		return nil, err
	}
	diff, err := gitRaw(ctx, w.runner, w.Path, "diff", "-z", "--no-renames", "--name-only", base)
	if err != nil {
		return nil, err
	}
	untracked, err := gitRaw(ctx, w.runner, w.Path, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return uniq(append(SplitZ(diff), SplitZ(untracked)...)), nil
}

// Existing keeps the paths that exist in the worktree.
func (w *Worktree) Existing(paths []string) []string {
	var found []string
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(w.Path, p)); err == nil {
			found = append(found, p)
		}
	}
	return found
}

func (w *Worktree) gitFile(ctx context.Context, name string) (string, error) {
	dir, err := git(ctx, w.runner, w.Path, "rev-parse", "--absolute-git-dir")
	return filepath.Join(dir, name), err
}
