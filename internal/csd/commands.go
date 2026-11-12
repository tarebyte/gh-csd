package csd

import (
	"bytes"
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

// IO is where commands write: Out for results, Err for progress, and
// whether Out is a terminal (tables and colors).
type IO struct {
	Out, Err io.Writer
	TTY      bool
	Width    int
}

// Commands are gh csd's commands.
type Commands struct {
	runner     Runner
	clock      Clock
	settings   *Settings
	prompt     Prompt
	io         IO
	codespaces *Codespaces
}

func NewCommands(r Runner, clock Clock, s *Settings, p Prompt, out IO, open func(string) error) *Commands {
	return &Commands{r, clock, s, p, out, &Codespaces{runner: r, clock: clock, Open: open, log: out.Err}}
}

// Up creates or reuses the codespace of branch's worktree ("" means the
// worktree at cwd). With status, it also waits for the dotfiles.
func (c *Commands) Up(branch string, status bool) error {
	tree, err := c.worktree(branch)
	if err != nil {
		return err
	}
	created, err := tree.Create()
	if err != nil {
		return err
	}
	if !created {
		c.log("reusing worktree %s", tree.Path)
	}
	name, err := c.reusableCodespace(tree)
	if err == nil && name == "" {
		name, err = c.createCodespace(tree, status)
	}
	if err != nil {
		return err
	}
	if _, err := c.sync(tree, name); err != nil {
		return err
	}
	fmt.Fprintf(c.io.Out, "gh csd: ready  worktree=%s  codespace=%s\n", tree.Path, name)
	return nil
}

// Run syncs the worktree at cwd, then runs args in its codespace and
// returns their exit status. No args only syncs.
func (c *Commands) Run(args []string) (int, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return 0, err
	}
	tree, err := Current(c.runner, c.settings.Cwd, c.settings.Branch())
	if err != nil {
		return 0, err
	}
	name := c.settings.Codespace()
	if name == "" {
		name = tree.Codespace()
	}
	if name == "" {
		return 0, fmt.Errorf("no codespace for %s; run gh csd up first, or pass -c", tree.Path)
	}
	remote, err := c.sync(tree, name)
	if err != nil || len(args) == 0 {
		return 0, err
	}
	return remote.Run(args)
}

// StatusOptions are status's output flags, as gh's --json, --jq, and
// --template.
type StatusOptions struct {
	Fields   []string
	JQ       string
	Template string
}

// Status lists each linked worktree and its codespace.
func (c *Commands) Status(opts StatusOptions) error {
	rows, err := c.statusRows()
	if err != nil {
		return err
	}
	if len(opts.Fields) > 0 {
		return c.printJSON(rows, opts)
	}
	if len(rows) == 0 {
		fmt.Fprintln(c.io.Err, "gh csd: no worktrees")
		return nil
	}
	table := tableprinter.New(c.io.Out, c.io.TTY, c.io.Width)
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
// the worktree at cwd, after asking).
func (c *Commands) Down(branch string, force bool) error {
	tree, err := c.downTarget(branch)
	if err != nil {
		return err
	}
	if !tree.Exist() {
		return fmt.Errorf("no worktree at %s", tree.Path)
	}
	if !force {
		work, err := tree.UnsavedWork()
		if err != nil {
			return err
		}
		if work != "" {
			return fmt.Errorf("%s has work that would be lost; --force deletes it anyway\n%s", tree.Branch, work)
		}
	}
	if name := tree.Codespace(); name != "" && c.codespaces.Exist(name) {
		if err := c.codespaces.Delete(name); err != nil {
			return err
		}
		NewRemote(c.runner, name, c.settings).Forget()
		c.log("deleted codespace %s", name)
	}
	if err := tree.Remove(force); err != nil {
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

func (c *Commands) worktree(branch string) (*Worktree, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return nil, err
	}
	if branch == "" {
		return Here(c.runner, c.settings.Cwd, c.settings.Branch())
	}
	return ForBranch(c.runner, branch, c.settings.Cwd, c.settings.Branch())
}

func (c *Commands) downTarget(branch string) (*Worktree, error) {
	if branch != "" {
		return c.worktree(branch)
	}
	if !c.prompt.Interactive() {
		return nil, fmt.Errorf("pass the branch to down when not in a terminal")
	}
	tree, err := c.worktree("")
	if err != nil {
		return nil, err
	}
	what := fmt.Sprintf("worktree %s and branch %s", tree.Path, tree.Branch)
	if name := tree.Codespace(); name != "" {
		what = "codespace " + name + ", " + what
	}
	ok, err := c.prompt.Confirm("Delete " + what + "?")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrCancelled
	}
	return tree, nil
}

func (c *Commands) reusableCodespace(tree *Worktree) (string, error) {
	name := tree.Codespace()
	if name == "" || !c.codespaces.Exist(name) {
		return "", nil
	}
	c.log("reusing codespace %s", name)
	// A stopped codespace restarts on connect, and its services start after it.
	if err := c.waitForServices(tree, name); err != nil {
		return "", err
	}
	return name, tree.SetPhase("")
}

func (c *Commands) createCodespace(tree *Worktree, status bool) (string, error) {
	defer tree.SetPhase("")
	if err := c.chooseMachine(); err != nil {
		return "", err
	}
	c.log("creating codespace on %s (a few minutes)...", c.settings.Machine())
	tree.SetPhase("creating…")
	name, err := c.codespaces.Create(tree.Branch, c.settings)
	if err != nil {
		return "", err
	}
	if err := tree.Bind(name); err != nil {
		return "", err
	}
	return name, c.waitUntilReady(tree, name, status)
}

// chooseMachine works as gh codespace create: the only machine type without
// asking, else ask in a terminal, else fail with the flag to pass.
func (c *Commands) chooseMachine() error {
	if c.settings.Machine() != "" {
		return nil
	}
	names, err := c.codespaces.Machines(c.settings)
	if err != nil {
		return err
	}
	switch {
	case len(names) == 0:
		return fmt.Errorf("%s has no machine types available", c.settings.Repo())
	case len(names) == 1:
		c.settings.SetMachine(names[0])
	case c.prompt.Interactive():
		name, err := c.prompt.Choose("Choose a machine type:", names)
		if err != nil {
			return err
		}
		c.settings.SetMachine(name)
	default:
		return fmt.Errorf("pass -m with one of: %s", strings.Join(names, ", "))
	}
	return nil
}

func (c *Commands) waitUntilReady(tree *Worktree, name string, status bool) error {
	c.log("waiting for codespace setup...")
	tree.SetPhase("setting up…")
	ready, err := c.waitForFile(name, readyMarker, 20*time.Minute, 15*time.Second)
	if err != nil {
		return err
	}
	if !ready {
		c.log("no ready marker after 20m; continuing")
	}
	if err := c.waitForServices(tree, name); err != nil || !status {
		return err
	}
	c.log("waiting for the dotfiles to finish...")
	tree.SetPhase("dotfiles…")
	done, err := c.waitForFile(name, doneMarker, 30*time.Minute, 30*time.Second)
	if err == nil {
		c.log(map[bool]string{true: "dotfiles finished", false: "dotfiles not finished after 30m; continuing"}[done])
	}
	return err
}

func (c *Commands) waitForFile(name, path string, timeout, interval time.Duration) (bool, error) {
	remote := NewRemote(c.runner, name, c.settings)
	return Wait(c.clock, timeout, interval, func() (bool, error) { return remote.FileExists(path) })
}

// waitForServices: services start with the container but take a while to
// accept connections, and the first test after up can fail without it.
func (c *Commands) waitForServices(tree *Worktree, name string) error {
	check := c.settings.ReadyCheck()
	if check == "" {
		return nil
	}
	c.log("waiting for services...")
	tree.SetPhase("starting services…")
	remote := NewRemote(c.runner, name, c.settings)
	ready, err := Wait(c.clock, 5*time.Minute, 5*time.Second, func() (bool, error) {
		res, err := remote.Capture(check, "")
		if errors.Is(err, ErrNoSSHServer) {
			return false, err
		}
		return err == nil && res.OK(), nil
	})
	if err == nil && !ready {
		c.log("services not ready after 5m; continuing")
	}
	return err
}

func (c *Commands) statusRows() ([]map[string]string, error) {
	if err := c.settings.RequireRepo(); err != nil {
		return nil, err
	}
	states, err := c.codespaces.States(c.settings)
	if err != nil {
		return nil, err
	}
	trees, err := All(c.runner, c.settings.Cwd, c.settings.Branch())
	if err != nil {
		return nil, err
	}
	rows := []map[string]string{}
	for _, tree := range trees {
		name, state := tree.Codespace(), ""
		if name != "" {
			if state = states[name]; state == "" {
				state = "Deleted"
			}
		}
		rows = append(rows, map[string]string{"worktree": tree.Path, "branch": tree.Branch,
			"base": c.settings.Branch(), "codespace": name, "state": state, "phase": tree.Phase()})
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
		return jq.Evaluate(bytes.NewReader(data), c.io.Out, opts.JQ)
	case opts.Template != "":
		t := template.New(c.io.Out, c.io.Width, c.io.TTY)
		if err := t.Parse(opts.Template); err != nil {
			return err
		}
		if err := t.Execute(bytes.NewReader(data)); err != nil {
			return err
		}
		return t.Flush()
	default:
		return jsonpretty.Format(c.io.Out, bytes.NewReader(data), "  ", c.io.TTY)
	}
}

func (c *Commands) sync(tree *Worktree, name string) (*Remote, error) {
	remote := NewRemote(c.runner, name, c.settings)
	err := (&Sync{c.runner, remote, c.settings.Takeover(), c.io.Err}).Call(tree)
	return remote, err
}

func (c *Commands) log(format string, args ...any) {
	fmt.Fprintf(c.io.Err, "gh csd: "+format+"\n", args...)
}
