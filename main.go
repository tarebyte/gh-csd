// Command gh-csd is the gh csd extension: edit a GitHub repo in a local
// worktree, and run it in that worktree's own codespace. The commands are
// in cmd, and the work in internal/csd.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/tarebyte/gh-csd/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cmd.Execute(ctx, os.Args[1:], os.Stderr)
	stop()
	os.Exit(code)
}
