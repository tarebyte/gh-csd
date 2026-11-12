package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/tarebyte/gh-csd/internal/csd"
)

// newInitCmd is gh csd init.
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write a commented config file",
		Long:  "Write a commented config file. Never replaces an existing one.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The config may not exist or parse yet, so init reads only the
			// environment.
			s := csd.NewSettings(environment(), "", nil, nil, "", "")
			return csd.NewCommands(s, csd.Deps{IO: csd.IO{Out: os.Stdout, Err: os.Stderr}}).Init()
		},
	}
}
