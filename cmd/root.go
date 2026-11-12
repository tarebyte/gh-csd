// Package cmd holds gh csd's cobra commands, one file per command, as gh
// lays out its own (pkg/cmd/codespace). Each file parses flags and calls
// internal/csd, which does the work.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/spf13/cobra"
	"github.com/tarebyte/gh-csd/internal/csd"
)

// exitError carries an exit status other than 1, such as the remote
// command's from run.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// Execute runs gh csd with args and returns its exit status, as gh's:
// 0 done, 1 failed, 2 cancelled.
func Execute(ctx context.Context, args []string, stderr io.Writer) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	var exit exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.code
	case errors.Is(err, csd.ErrCancelled), errors.Is(err, terminal.InterruptErr), errors.Is(err, context.Canceled):
		return 2
	default:
		fmt.Fprintln(stderr, "gh csd:", err)
		return 1
	}
}

// NewRootCmd builds the command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "csd <command>",
		Short:         "Edit a GitHub repo in a local worktree, and run it in that worktree's own codespace",
		Long:          "Edit a GitHub repo in a local worktree, and run it in that worktree's own codespace.\n\nSettings come from flags, then ~/.config/gh-csd/config.yml, then defaults.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "gh csd"},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\n\n%s", err, cmd.UsageString())
	})
	root.AddCommand(newUpCmd(), newRunCmd(), newStatusCmd(), newDownCmd(), newInitCmd())
	return root
}
