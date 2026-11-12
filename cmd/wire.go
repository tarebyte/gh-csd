package cmd

import (
	"context"
	"os"
	"path/filepath"

	gh "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/ssh"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/tarebyte/gh-csd/internal/csd"
)

// environment is the part of the environment Settings reads, with HOME
// from the OS rather than the variable.
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
	// Codespaces exist only on github.com, whatever gh's default host
	// (GH_HOST) is; a 404 from another server would read as "deleted".
	client, err := api.NewRESTClient(api.ClientOptions{Host: "github.com"})
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
