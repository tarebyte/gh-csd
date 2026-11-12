package csd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// OwnerFile marks which worktree owns a codespace. Kept from the shell
	// version so its codespaces stay compatible.
	OwnerFile   = ".git/cs-dev-owner"
	resetScript = `xargs -0 sh -c 'for p; do ` +
		`if git cat-file -e HEAD:"$p" 2>/dev/null; then git checkout -q HEAD -- "$p"; else rm -f -- "$p"; fi; ` +
		`done' _`
)

// Sync makes a codespace's checkout match a worktree's changes.
type Sync struct {
	remote   *Remote
	takeover bool
	log      io.Writer
}

// Call copies tree's change set to the codespace, deletes what was deleted
// locally, and resets what is no longer changed.
func (s *Sync) Call(ctx context.Context, tree *Worktree) error {
	if err := s.claim(ctx, tree); err != nil {
		return err
	}
	changed, err := tree.ChangedPaths(ctx)
	if err != nil {
		return err
	}
	dirty, err := s.remoteDirty(ctx)
	if err != nil {
		return err
	}
	set := NewChangeSet(changed, tree.Existing(changed), dirty)
	if err := s.apply(ctx, set, tree.Path); err != nil {
		return err
	}
	fmt.Fprintf(s.log, "gh csd: copied %d, deleted %d, reset %d\n", len(set.Sync), len(set.Gone), len(set.Reset))
	return nil
}

// claim keeps one codespace to one worktree: the reset step would undo
// another worktree's synced files. The binding is saved only once the
// claim succeeds.
func (s *Sync) claim(ctx context.Context, tree *Worktree) error {
	me := shortHostname() + ":" + tree.Path
	owner, err := s.remote.Capture(ctx, "cat "+s.ownerFile()+" 2>/dev/null", "")
	if err != nil {
		return err
	}
	if o := strings.TrimSpace(owner.Out); o != "" && o != me && !s.takeover {
		return fmt.Errorf("%s is in use by %s; use another codespace, or pass --takeover", s.remote.Codespace, o)
	}
	res, err := s.remote.Capture(ctx, "echo "+ShellEscape(me)+" > "+s.ownerFile(), "")
	if err := check(res, err, "claim"); err != nil {
		return err
	}
	return tree.Bind(ctx, s.remote.Codespace)
}

// shortHostname matches `hostname -s`, which the Ruby version wrote.
func shortHostname() string {
	host, _ := os.Hostname()
	short, _, _ := strings.Cut(host, ".")
	return short
}

func (s *Sync) remoteDirty(ctx context.Context) ([]string, error) {
	res, err := s.remote.Capture(ctx, "cd "+ShellEscape(s.remote.Dir)+" && git status --porcelain -z --no-renames --untracked-files=all", "")
	if err := check(res, err, "git status"); err != nil {
		return nil, err
	}
	return ParsePorcelain(res.Out), nil
}

func (s *Sync) apply(ctx context.Context, set ChangeSet, fromDir string) error {
	res, err := s.remote.Copy(ctx, set.Sync, fromDir)
	if err := check(res, err, "rsync"); err != nil {
		return err
	}
	if err := s.pipePaths(ctx, "xargs -0 rm -f --", set.Gone, "delete"); err != nil {
		return err
	}
	return s.pipePaths(ctx, resetScript, set.Reset, "reset")
}

func (s *Sync) pipePaths(ctx context.Context, script string, paths []string, step string) error {
	if len(paths) == 0 {
		return nil
	}
	res, err := s.remote.Capture(ctx, "cd "+ShellEscape(s.remote.Dir)+" && "+script, strings.Join(paths, "\x00")+"\x00")
	return check(res, err, step)
}

func (s *Sync) ownerFile() string { return ShellEscape(s.remote.Dir) + "/" + OwnerFile }

func check(res Result, err error, step string) error {
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s failed in the codespace: %s", step, strings.TrimSpace(res.Err))
	}
	return nil
}
