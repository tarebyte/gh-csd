package cmd

import (
	"github.com/spf13/cobra"
)

// newRunCmd is gh csd run.
func newRunCmd() *cobra.Command {
	var codespace string
	var takeover bool
	cmd := &cobra.Command{
		Use:   "run [flags] [--] [<command> [<args>...]]",
		Short: "Sync the worktree's changes to its codespace, then run a command there",
		Long: `Copy the worktree's changes to its codespace, then run a command there.
With no command, run only syncs. The command runs in a non-login shell, as
with gh codespace ssh, and run exits with the command's exit status.`,
		Example: "  $ gh csd run bin/test test/user_test.rb\n  $ gh csd run -- bash -lc 'git fetch origin'",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := map[string]string{"codespace": codespace}
			if takeover {
				flags["takeover"] = "true"
			}
			c, err := commands(cmd.Context(), flags)
			if err != nil {
				return err
			}
			code, err := c.Run(cmd.Context(), args)
			if err == nil && code != 0 {
				return exitError{code}
			}
			return err
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVarP(&codespace, "codespace", "c", "", "Use this codespace instead of the worktree's own")
	cmd.Flags().BoolVar(&takeover, "takeover", false, "Claim a codespace that another worktree owns")
	return cmd
}
