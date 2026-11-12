// Command gh-csd is the gh csd extension: edit a GitHub repo in a local
// worktree, and run it in that worktree's own codespace. This file is
// flags, help, exit codes, and wiring; the work is in internal/csd.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AlecAivazis/survey/v2/terminal"
	gh "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/ssh"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"
	"github.com/tarebyte/gh-csd/internal/csd"
)

// exitError carries an exit status other than 1, such as the remote
// command's from run.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := execute(ctx, os.Args[1:], os.Stderr)
	stop()
	os.Exit(code)
}

// execute runs gh csd with args and returns its exit status, as gh's:
// 0 done, 1 failed, 2 cancelled.
func execute(ctx context.Context, args []string, stderr io.Writer) int {
	root := rootCmd()
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

func rootCmd() *cobra.Command {
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
	root.AddCommand(upCmd(), runCmd(), statusCmd(), downCmd(), initCmd())
	return root
}

func upCmd() *cobra.Command {
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
			flags := map[string]string{"machine": machine, "devcontainer": devcontainer}
			if defaultPermissions {
				flags["default_permissions"] = "true"
			}
			c, err := commands(cmd.Context(), flags)
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

func runCmd() *cobra.Command {
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

func statusCmd() *cobra.Command {
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

func downCmd() *cobra.Command {
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

func initCmd() *cobra.Command {
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

func environment() map[string]string {
	env := map[string]string{}
	for _, key := range csd.EnvKeys {
		env[key] = os.Getenv(key)
	}
	if home, err := os.UserHomeDir(); err == nil {
		env["HOME"] = home
	}
	return env
}

// commands wires the real runner, REST client, terminal, and browser.
func commands(ctx context.Context, flags map[string]string) (*csd.Commands, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ghPath, err := gh.Path()
	if err != nil {
		return nil, err
	}
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	runner := csd.ExecRunner{}
	settings, err := csd.ForCheckout(ctx, environment(), cwd, flags, runner, ssh.NewTranslator().Translate)
	if err != nil {
		return nil, err
	}
	settings.CacheDir = filepath.Join(cache, "gh-csd")
	t := term.FromEnv()
	width, _, err := t.Size()
	if err != nil {
		width = 80
	}
	return csd.NewCommands(settings, csd.Deps{
		Runner: runner,
		API:    client,
		GH:     ghPath,
		Clock:  csd.NewRealClock(),
		Prompt: newPrompt(),
		IO: csd.IO{Out: t.Out(), Err: t.ErrOut(), TTY: t.IsTerminalOutput(), Color: t.IsColorEnabled(),
			StdinTTY: term.IsTerminal(os.Stdin), Width: width},
		Open: browser.New("", os.Stderr, os.Stderr).Browse,
	}), nil
}

// termPrompt asks with gh's prompter, on stderr so stdout stays clean.
type termPrompt struct {
	p           *prompter.Prompter
	interactive bool
}

func newPrompt() termPrompt {
	return termPrompt{prompter.New(os.Stdin, os.Stderr, os.Stderr), term.IsTerminal(os.Stdin) && term.IsTerminal(os.Stderr)}
}

func (t termPrompt) Interactive() bool { return t.interactive }

func (t termPrompt) Choose(question string, options []string) (string, error) {
	i, err := t.p.Select(question, "", options)
	if err != nil {
		return "", err
	}
	return options[i], nil
}

func (t termPrompt) Confirm(question string) (bool, error) { return t.p.Confirm(question, false) }
