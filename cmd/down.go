package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// newDownCmd is gh csd down.
func newDownCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "down [<branch>]",
		Short: "Delete a worktree's codespace, the worktree, and its branch",
		Long: `Delete a worktree's codespace, then the worktree and its local branch.
Refuses when the branch has work that no remote has.

With no branch, down uses the worktree you are in and asks first.`,
		Example: "  $ gh csd down me/fix-login",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := commands(cmd.Context(), nil)
			if err != nil {
				return err
			}
			return c.Down(cmd.Context(), strings.Join(args, ""), force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Delete even when the branch has uncommitted or unpushed work")
	return cmd
}
