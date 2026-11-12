// Package csd pairs a local git worktree with its own GitHub codespace.
package csd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
)

// Result is a finished command's output and exit status.
type Result struct {
	Out, Err string
	Status   int
}

// OK reports a zero exit status.
func (r Result) OK() bool { return r.Status == 0 }

// Runner runs every external command, so tests can swap in a fake.
type Runner interface {
	// Capture runs cmd with stdin and collects its output.
	Capture(cmd []string, stdin string) Result
	// Run attaches cmd to the terminal, for the user's own command.
	Run(cmd []string) int
}

// ExecRunner runs commands for real.
type ExecRunner struct{}

func (ExecRunner) Capture(cmd []string, stdin string) Result {
	c := exec.Command(cmd[0], cmd[1:]...)
	var out, errOut bytes.Buffer
	c.Stdin = bytes.NewBufferString(stdin)
	c.Stdout, c.Stderr = &out, &errOut
	code := status(c.Run(), &errOut)
	return Result{out.String(), errOut.String(), code}
}

func (ExecRunner) Run(cmd []string) int {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return status(c.Run(), nil)
}

// status turns a run error into an exit status: 127 when the program
// could not start, as a shell reports it.
func status(err error, errOut *bytes.Buffer) int {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		if errOut != nil {
			errOut.WriteString(err.Error())
		}
		return 127
	}
}
