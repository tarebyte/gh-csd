package csd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/jq"
	"github.com/cli/go-gh/v2/pkg/jsonpretty"
	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"github.com/cli/go-gh/v2/pkg/template"
)

const (
	readyMarker = "~/.local/state/gh-csd/setup-ready"
	doneMarker  = "~/.local/state/gh-csd/setup-complete"
)

// JSONFields are the fields of status --json, in output order.
var JSONFields = []string{"worktree", "branch", "base", "codespace", "state", "phase"}

// ErrCancelled means the person said no; gh exits 2 for it.
var ErrCancelled = errors.New("cancelled")

// Prompt asks the person at the terminal. Callers check Interactive first
// and fail with the flag to pass instead, as gh does without a terminal.
type Prompt interface {
	Interactive() bool
	Choose(question string, options []string) (string, error)
	Confirm(question string) (bool, error)
}

// IO is where commands write: Out for results and Err for progress. TTY and
// Color describe Out; StdinTTY says whether run can attach a terminal.
type IO struct {
	Out, Err io.Writer
	TTY      bool
	Color    bool
	StdinTTY bool
	Width    int
}

// Deps are what commands use beyond their settings; main wires the real
// ones and tests the fakes.
type Deps struct {
	Runner Runner
	API    API
	// GH is the gh executable, the same one that runs this extension.
	GH     string
	Clock  Clock
	Prompt Prompt
	IO     IO
	// Open opens a URL in the browser.
	Open func(url string) error
}

// Commands are gh csd's commands.
type Commands struct {
	Deps
	settings   *Settings
	codespaces *Codespaces
}

// NewCommands builds the commands for settings.
func NewCommands(s *Settings, d Deps) *Commands {
	return &Commands{d, s, &Codespaces{runner: d.Runner, api: d.API, gh: d.GH, clock: d.Clock, Open: d.Open, log: d.IO.Err}}
}

// Up creates or reuses the codespace of branch's worktree ("" means the
// worktree at cwd). With status, it also waits for the dotfiles.
func (c *Commands) Up(ctx context.Context, branch string, status bool) error {
	tree, err := c.worktree(ctx, branch)
	if err != nil {
		return err
	}
	created, err := tree.Create(ctx)
	if err != nil {
		return err
	}
	if !created {
		c.log("reusing worktree %s", tree.Path)
	}
	name, err := c.reusableCodespace(ctx, tree)
	if err == nil && name == "" {
		name, err = c.createCodespace(ctx, tree, status)
	}
	if err != nil {
		return err
	}
	if _, err := c.sync(ctx, tree, name); err != nil {
		return err
	}
	fmt.Fprintf(c.IO.Out, "gh csd: ready  worktree=%s  codespace=%s\n", tree.Path, name)
	return nil
}

// Run syncs the worktree at cwd, then runs args in its codespace and
// returns their exit status. No args only syncs.
func (c *Commands) Run(ctx context.Context, args []string) (int, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return 0, err
	}
	tree, err := Current(ctx, c.Runner, c.settings.Cwd, c.settings.Branch())
	if err != nil {
		return 0, err
	}
	name := c.settings.Codespace()
	if name == "" {
		name = tree.Codespace(ctx)
	}
	if name == "" {
		return 0, fmt.Errorf("no codespace for %s; run gh csd up first, or pass -c", tree.Path)
	}
	remote, err := c.sync(ctx, tree, name)
	if err != nil || len(args) == 0 {
		return 0, err
	}
	return remote.Run(ctx, args)
}

// StatusOptions are status's output flags, as gh's --json, --jq, and
// --template.
type StatusOptions struct {
	Fields   []string
	JQ       string
	Template string
}

// Status lists each linked worktree and its codespace.
func (c *Commands) Status(ctx context.Context, opts StatusOptions) error {
	rows, err := c.statusRows(ctx)
	if err != nil {
		return err
	}
	if len(opts.Fields) > 0 {
		return c.printJSON(rows, opts)
	}
	if len(rows) == 0 {
		fmt.Fprintln(c.IO.Err, "gh csd: no worktrees")
		return nil
	}
	table := tableprinter.New(c.IO.Out, c.IO.TTY, c.IO.Width)
	table.AddHeader([]string{"BRANCH", "CODESPACE", "STATE", "PHASE", "WORKTREE"})
	for _, row := range rows {
		for _, field := range []string{"branch", "codespace", "state", "phase", "worktree"} {
			table.AddField(row[field])
		}
		table.EndRow()
	}
	return table.Render()
}

// Down deletes branch's codespace, worktree, and local branch ("" means
// the worktree at cwd, after asking). It stops before removing anything
// when it cannot tell whether the codespace still exists, so a codespace
// is never orphaned.
func (c *Commands) Down(ctx context.Context, branch string, force bool) error {
	tree, err := c.downTarget(ctx, branch)
	if err != nil {
		return err
	}
	if !tree.Exist() {
		return fmt.Errorf("no worktree at %s", tree.Path)
	}
	if !force {
		work, err := tree.UnsavedWork(ctx)
		if err != nil {
			return err
		}
		if work != "" {
			return fmt.Errorf("%s has work that would be lost; --force deletes it anyway\n%s", tree.Branch, work)
		}
	}
	if name := tree.Codespace(ctx); name != "" {
		exists, err := c.codespaces.Exist(ctx, name)
		if err != nil {
			return err
		}
		if exists {
			if err := c.codespaces.Delete(ctx, name); err != nil {
				return err
			}
			c.log("deleted codespace %s", name)
		}
		c.remote(name).Forget()
	}
	if err := tree.Remove(ctx, force); err != nil {
		return err
	}
	c.log("removed worktree=%s branch=%s", tree.Path, tree.Branch)
	return nil
}

// Init writes the commented template config; never replaces a file.
func (c *Commands) Init() error {
	path := c.settings.ConfigPath()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(Template), 0o644); err != nil {
		return err
	}
	c.log("wrote %s", path)
	return nil
}

func (c *Commands) worktree(ctx context.Context, branch string) (*Worktree, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return nil, err
	}
	if branch == "" {
		return Here(ctx, c.Runner, c.settings.Cwd, c.settings.Branch())
	}
	return ForBranch(ctx, c.Runner, branch, c.settings.Cwd, c.settings.Branch())
}

func (c *Commands) downTarget(ctx context.Context, branch string) (*Worktree, error) {
	if branch != "" {
		return c.worktree(ctx, branch)
	}
	if !c.Prompt.Interactive() {
		return nil, errors.New("pass the branch to down when not in a terminal")
	}
	tree, err := c.worktree(ctx, "")
	if err != nil {
		return nil, err
	}
	what := fmt.Sprintf("worktree %s and branch %s", tree.Path, tree.Branch)
	if name := tree.Codespace(ctx); name != "" {
		what = "codespace " + name + ", " + what
	}
	ok, err := c.Prompt.Confirm("Delete " + what + "?")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrCancelled
	}
	return tree, nil
}

func (c *Commands) reusableCodespace(ctx context.Context, tree *Worktree) (string, error) {
	name := tree.Codespace(ctx)
	if name == "" {
		return "", nil
	}
	exists, err := c.codespaces.Exist(ctx, name)
	if err != nil || !exists {
		return "", err
	}
	c.log("reusing codespace %s", name)
	// A stopped codespace restarts on connect, and its services start after it.
	if err := c.waitForServices(ctx, tree, name); err != nil {
		return "", err
	}
	tree.SetPhase(ctx, "")
	return name, nil
}

func (c *Commands) createCodespace(ctx context.Context, tree *Worktree, status bool) (string, error) {
	// Cleared even after Ctrl-C, which cancels ctx.
	defer tree.SetPhase(context.WithoutCancel(ctx), "")
	if err := c.chooseMachine(ctx); err != nil {
		return "", err
	}
	c.log("creating codespace on %s (a few minutes)...", c.settings.Machine())
	tree.SetPhase(ctx, "creating…")
	name, err := c.codespaces.Create(ctx, tree.Branch, c.settings)
	if err != nil {
		return "", err
	}
	if err := tree.Bind(ctx, name); err != nil {
		return "", err
	}
	return name, c.waitUntilReady(ctx, tree, name, status)
}

// chooseMachine works as gh codespace create: the only machine type without
// asking, else ask in a terminal, else fail with the flag to pass.
func (c *Commands) chooseMachine(ctx context.Context) error {
	if c.settings.Machine() != "" {
		return nil
	}
	names, err := c.codespaces.Machines(ctx, c.settings)
	if err != nil {
		return err
	}
	switch {
	case len(names) == 0:
		return fmt.Errorf("%s has no machine types available", c.settings.Repo())
	case len(names) == 1:
		c.settings.SetMachine(names[0])
	case c.Prompt.Interactive():
		name, err := c.Prompt.Choose("Choose a machine type:", names)
		if err != nil {
			return err
		}
		c.settings.SetMachine(name)
	default:
		return fmt.Errorf("pass -m with one of: %s", strings.Join(names, ", "))
	}
	return nil
}

func (c *Commands) waitUntilReady(ctx context.Context, tree *Worktree, name string, status bool) error {
	c.log("waiting for codespace setup...")
	tree.SetPhase(ctx, "setting up…")
	ready, err := c.waitForFile(ctx, name, readyMarker, 20*time.Minute, 15*time.Second)
	if err != nil {
		return err
	}
	if !ready {
		c.log("no ready marker after 20m; continuing")
	}
	if err := c.waitForServices(ctx, tree, name); err != nil || !status {
		return err
	}
	c.log("waiting for the dotfiles to finish...")
	tree.SetPhase(ctx, "dotfiles…")
	done, err := c.waitForFile(ctx, name, doneMarker, 30*time.Minute, 30*time.Second)
	if err != nil {
		return err
	}
	if done {
		c.log("dotfiles finished")
	} else {
		c.log("dotfiles not finished after 30m; continuing")
	}
	return nil
}

func (c *Commands) waitForFile(ctx context.Context, name, path string, timeout, interval time.Duration) (bool, error) {
	remote := c.remote(name)
	return Wait(ctx, c.Clock, timeout, interval, func() (bool, error) { return remote.FileExists(ctx, path) })
}

// waitForServices: services start with the container but take a while to
// accept connections, and the first test after up can fail without it.
func (c *Commands) waitForServices(ctx context.Context, tree *Worktree, name string) error {
	check := c.settings.ReadyCheck()
	if check == "" {
		return nil
	}
	c.log("waiting for services...")
	tree.SetPhase(ctx, "starting services…")
	remote := c.remote(name)
	ready, err := Wait(ctx, c.Clock, 5*time.Minute, 5*time.Second, func() (bool, error) {
		res, err := remote.Capture(ctx, check, "")
		if errors.Is(err, ErrNoSSHServer) || ctx.Err() != nil {
			return false, err
		}
		return err == nil && res.OK(), nil
	})
	if err == nil && !ready {
		c.log("services not ready after 5m; continuing")
	}
	return err
}

func (c *Commands) statusRows(ctx context.Context) ([]map[string]string, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return nil, err
	}
	states, err := c.codespaces.States(ctx, c.settings)
	if err != nil {
		return nil, err
	}
	trees, err := All(ctx, c.Runner, c.settings.Cwd, c.settings.Branch())
	if err != nil {
		return nil, err
	}
	rows := []map[string]string{}
	for _, tree := range trees {
		name, state := tree.Codespace(ctx), ""
		if name != "" {
			if state = states[name]; state == "" {
				state = "Deleted"
			}
		}
		rows = append(rows, map[string]string{"worktree": tree.Path, "branch": tree.Branch,
			"base": c.settings.Branch(), "codespace": name, "state": state, "phase": tree.Phase(ctx)})
	}
	return rows, nil
}

func (c *Commands) printJSON(rows []map[string]string, opts StatusOptions) error {
	selected := make([]map[string]string, len(rows))
	for i, row := range rows {
		selected[i] = map[string]string{}
		for _, f := range opts.Fields {
			selected[i][f] = row[f]
		}
	}
	data, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	switch {
	case opts.JQ != "":
		return jq.Evaluate(bytes.NewReader(data), c.IO.Out, opts.JQ)
	case opts.Template != "":
		t := template.New(c.IO.Out, c.IO.Width, c.IO.Color)
		if err := t.Parse(opts.Template); err != nil {
			return err
		}
		if err := t.Execute(bytes.NewReader(data)); err != nil {
			return err
		}
		return t.Flush()
	default:
		return jsonpretty.Format(c.IO.Out, bytes.NewReader(data), "  ", c.IO.Color)
	}
}

func (c *Commands) remote(name string) *Remote {
	return NewRemote(c.Runner, c.GH, name, c.settings, c.IO.StdinTTY)
}

func (c *Commands) sync(ctx context.Context, tree *Worktree, name string) (*Remote, error) {
	remote := c.remote(name)
	err := (&Sync{remote, c.settings.Takeover(), c.IO.Err}).Call(ctx, tree)
	return remote, err
}

func (c *Commands) log(format string, args ...any) {
	fmt.Fprintf(c.IO.Err, "gh csd: "+format+"\n", args...)
}
