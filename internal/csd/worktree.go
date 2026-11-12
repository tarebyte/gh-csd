package csd

import (
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
func ForBranch(r Runner, branch, cwd, base string) (*Worktree, error) {
	main, err := mainOf(r, cwd)
	if err != nil {
		return nil, err
	}
	linked, err := linkedWorktrees(r, main)
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
func Here(r Runner, cwd, base string) (*Worktree, error) {
	top, err := git(r, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	branch, err := git(r, cwd, "branch", "--show-current")
	if err != nil {
		return nil, err
	}
	if branch == "" {
		return nil, fmt.Errorf("%s has no branch checked out; pass a branch", top)
	}
	tree, err := ForBranch(r, branch, cwd, base)
	if err != nil {
		return nil, err
	}
	if tree.Path != top {
		return nil, fmt.Errorf("%s is the main checkout; pass a branch, or run from a worktree", top)
	}
	return tree, nil
}

// All is every linked worktree of the checkout at cwd, in git's order.
func All(r Runner, cwd, base string) ([]*Worktree, error) {
	main, err := mainOf(r, cwd)
	if err != nil {
		return nil, err
	}
	linked, err := linkedWorktrees(r, main)
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
func Current(r Runner, cwd, base string) (*Worktree, error) {
	top, err := git(r, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	return &Worktree{runner: r, Path: top, base: base}, nil
}

// linkedWorktrees is [path, branch] of every worktree but the main
// checkout; branch is "" when detached. Never the main checkout: down
// would try to remove it.
func linkedWorktrees(r Runner, main string) ([][2]string, error) {
	out, err := git(r, main, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var found [][2]string
	for _, entry := range strings.Split(out, "\n\n") {
		lines := strings.Split(entry, "\n")
		path := strings.TrimPrefix(lines[0], "worktree ")
		branch := ""
		for _, line := range lines {
			if strings.HasPrefix(line, "branch ") {
				branch = strings.TrimPrefix(line, "branch refs/heads/")
			}
		}
		if path != main {
			found = append(found, [2]string{path, branch})
		}
	}
	return found, nil
}

func mainOf(r Runner, cwd string) (string, error) {
	common, err := git(r, cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return strings.TrimSuffix(common, "/.git"), err
}

func git(r Runner, dir string, args ...string) (string, error) {
	res := r.Capture(append([]string{"git", "-C", dir}, args...), "")
	if !res.OK() {
		return "", fmt.Errorf("git %s failed in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(res.Err))
	}
	return strings.TrimSpace(res.Out), nil
}

func (w *Worktree) Exist() bool {
	info, err := os.Stat(w.Path)
	return err == nil && info.IsDir()
}

// Create makes the worktree, and reports whether it was new.
func (w *Worktree) Create() (bool, error) {
	if _, err := git(w.runner, w.Main, "fetch", "-q", "origin", w.base); err != nil {
		return false, err
	}
	if w.Exist() {
		return false, nil
	}
	exists := w.runner.Capture([]string{"git", "-C", w.Main, "show-ref", "-q", "--verify", "refs/heads/" + w.Branch}, "").OK()
	args := []string{"worktree", "add", "-q", "-b", w.Branch, w.Path, "origin/" + w.base}
	if exists {
		args = []string{"worktree", "add", "-q", w.Path, w.Branch}
	}
	_, err := git(w.runner, w.Main, args...)
	return err == nil, err
}

// Codespace is the bound codespace's name, or "".
func (w *Worktree) Codespace() string { return w.readGitFile(BindingFile) }

func (w *Worktree) Bind(codespace string) error {
	return w.writeGitFile(BindingFile, codespace)
}

// Phase is what up is doing with the codespace ("" when nothing).
func (w *Worktree) Phase() string { return w.readGitFile(StatusFile) }

// SetPhase records it; "" clears it.
func (w *Worktree) SetPhase(phase string) error {
	path, err := w.gitFile(StatusFile)
	if err != nil {
		return err
	}
	if phase == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(phase+"\n"), 0o644)
}

// StatusPath is where the phase lives.
func (w *Worktree) StatusPath() (string, error) { return w.gitFile(StatusFile) }

// UnsavedWork is what removing the worktree and branch would lose:
// uncommitted or untracked files, and commits no remote has. "" when safe.
func (w *Worktree) UnsavedWork() (string, error) {
	dirty, err := git(w.runner, w.Path, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	unpushed, err := git(w.runner, w.Main, "log", "--oneline", w.Branch, "--not", "--remotes")
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
func (w *Worktree) Remove(force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	if _, err := git(w.runner, w.Main, append(args, w.Path)...); err != nil {
		return err
	}
	_, err := git(w.runner, w.Main, "branch", "-D", w.Branch)
	return err
}

// ChangedPaths differ from merge-base(HEAD, origin/<base>), plus untracked.
func (w *Worktree) ChangedPaths() ([]string, error) {
	base, err := git(w.runner, w.Path, "merge-base", "HEAD", "origin/"+w.base)
	if err != nil {
		return nil, err
	}
	diff := w.runner.Capture([]string{"git", "-C", w.Path, "diff", "-z", "--no-renames", "--name-only", base}, "").Out
	untracked := w.runner.Capture([]string{"git", "-C", w.Path, "ls-files", "-z", "--others", "--exclude-standard"}, "").Out
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

func (w *Worktree) gitFile(name string) (string, error) {
	dir, err := git(w.runner, w.Path, "rev-parse", "--absolute-git-dir")
	return filepath.Join(dir, name), err
}

func (w *Worktree) readGitFile(name string) string {
	path, err := w.gitFile(name)
	if err != nil {
		return ""
	}
	text, _ := os.ReadFile(path)
	return strings.TrimSpace(string(text))
}

func (w *Worktree) writeGitFile(name, value string) error {
	path, err := w.gitFile(name)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value+"\n"), 0o644)
}
