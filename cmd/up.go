package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newUpCmd is gh csd up.
func newUpCmd() *cobra.Command {
	var machine, devcontainer string
	var defaultPermissions, status bool
	cmd := &cobra.Command{
		Use:   "up [<branch>]",
		Short: "Create or reuse a codespace for a worktree",
		Long: `Create or reuse a codespace for a worktree, then sync the worktree to it.

With no branch, up uses the worktree you are in. With a branch, up uses that
branch's worktree, or creates one next to the main checkout. The codespace
checks out the base branch; run copies your changes onto it.`,
		Example: "  $ gh csd up\n  $ gh csd up me/fix-login -m standardLinux32gb",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := commands(cmd.Context(), upFlags(cmd, machine, devcontainer, defaultPermissions))
			if err != nil {
				return err
			}
			return c.Up(cmd.Context(), strings.Join(args, ""), status)
		},
	}
	cmd.Flags().StringVarP(&machine, "machine", "m", "", "Machine type; asks in a terminal when not set")
	cmd.Flags().StringVar(&devcontainer, "devcontainer-path", "", "Path to the devcontainer.json file to use")
	cmd.Flags().BoolVar(&defaultPermissions, "default-permissions", false, "Do not ask to approve the devcontainer's extra permissions")
	cmd.Flags().BoolVarP(&status, "status", "s", false, "Wait until the dotfiles finish too")
	return cmd
}

// upFlags are the settings up's flags set. An explicit
// --default-permissions=false overrides the config too.
func upFlags(cmd *cobra.Command, machine, devcontainer string, defaultPermissions bool) map[string]string {
	flags := map[string]string{"machine": machine, "devcontainer": devcontainer}
	if cmd.Flags().Changed("default-permissions") {
		flags["default_permissions"] = strconv.FormatBool(defaultPermissions)
	}
	return flags
}
