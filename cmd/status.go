package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tarebyte/gh-csd/internal/csd"
)

// newStatusCmd is gh csd status.
func newStatusCmd() *cobra.Command {
	var opts csd.StatusOptions
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Show each worktree and its codespace",
		Example: "  $ gh csd status\n  $ gh csd status --json branch,codespace,state\n  $ gh csd status --json branch,state --jq '.[] | select(.state == \"Available\") | .branch'",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkJSONFlags(opts); err != nil {
				return err
			}
			c, err := commands(cmd.Context(), nil)
			if err != nil {
				return err
			}
			return c.Status(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringSliceVar(&opts.Fields, "json", nil, "Output JSON with the specified fields")
	cmd.Flags().StringVarP(&opts.JQ, "jq", "q", "", "Filter JSON output using a jq expression")
	cmd.Flags().StringVarP(&opts.Template, "template", "t", "", "Format JSON output using a Go template; see \"gh help formatting\"")
	// As gh: --json without fields lists them.
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if err.Error() == "flag needs an argument: --json" {
			return errors.New(fieldsHelp())
		}
		return fmt.Errorf("%w\n\n%s", err, c.UsageString())
	})
	return cmd
}

// fieldsHelp is gh's answer to --json without fields.
func fieldsHelp() string {
	return "Specify one or more comma-separated fields for `--json`:\n  " + strings.Join(csd.JSONFields, "\n  ")
}

func checkJSONFlags(opts csd.StatusOptions) error {
	for _, f := range opts.Fields {
		if !slices.Contains(csd.JSONFields, f) {
			return fmt.Errorf("unknown JSON field: %q\n%s", f, fieldsHelp())
		}
	}
	if len(opts.Fields) == 0 && (opts.JQ != "" || opts.Template != "") {
		return errors.New("cannot use `--jq` or `--template` without specifying `--json`")
	}
	return nil
}
