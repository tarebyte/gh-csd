// Package csd pairs a local git worktree with its own GitHub codespace.
package csd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Result is a finished command's output and exit status.
type Result struct {
	Out, Err string
	Status   int
}

// OK reports a zero exit status.
func (r Result) OK() bool { return r.Status == 0 }

// Runner runs every external command, so tests can swap in a fake. Both
// methods return an error only when the program cannot start; a non-zero
// exit is a Result.
type Runner interface {
	// Capture runs cmd with stdin and collects its output.
	Capture(ctx context.Context, cmd []string, stdin string) (Result, error)
	// Run attaches cmd to the terminal, for the user's own command.
	Run(ctx context.Context, cmd []string) (int, error)
}

// ExecRunner runs commands for real.
type ExecRunner struct{}

// Capture implements Runner.
func (ExecRunner) Capture(ctx context.Context, cmd []string, stdin string) (Result, error) {
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	var out, errOut bytes.Buffer
	c.Stdin = strings.NewReader(stdin)
	c.Stdout, c.Stderr = &out, &errOut
	code, err := exitCode(ctx, c.Run(), cmd[0])
	return Result{out.String(), errOut.String(), code}, err
}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, cmd []string) (int, error) {
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return exitCode(ctx, c.Run(), cmd[0])
}

// exitCode reports a cancelled context as its error: a killed child's exit
// status (-1) would otherwise pass for the program's own.
func exitCode(ctx context.Context, err error, program string) (int, error) {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case ctx.Err() != nil:
		return 0, ctx.Err()
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	default:
		return 0, fmt.Errorf("running %s: %w", program, err)
	}
}
