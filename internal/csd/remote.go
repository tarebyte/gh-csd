package csd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrNoSSHServer means retrying never helps: the devcontainer has to
// install sshd.
var ErrNoSSHServer = errors.New("no SSH server")

// Remote talks to a codespace over plain ssh and rsync, using the config
// that `gh codespace ssh --config` prints.
type Remote struct {
	runner    Runner
	gh        string
	Codespace string
	Dir       string
	cacheDir  string
	home      string
	// TTY passes -t to ssh, for a command attached to a terminal.
	TTY  bool
	host string
}

// NewRemote talks to codespace through gh at ghPath.
func NewRemote(r Runner, ghPath, codespace string, s *Settings, tty bool) *Remote {
	return &Remote{runner: r, gh: ghPath, Codespace: codespace, Dir: s.RemoteDir(),
		cacheDir: s.CacheDir, home: s.Home(), TTY: tty}
}

func (r *Remote) cached() string { return filepath.Join(r.cacheDir, r.Codespace+".ssh") }

// ConfigPath is the ssh config, cached across runs in a directory only the
// user can read: fetching it goes through gh and costs seconds, and ssh -F
// trusts it.
func (r *Remote) ConfigPath(ctx context.Context) (string, error) {
	path := r.cached()
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	res, err := r.runner.Capture(ctx, []string{r.gh, "codespace", "ssh", "--config", "-c", r.Codespace}, "")
	if err != nil {
		return "", err
	}
	if strings.Contains(res.Err, "failed to start SSH server") {
		return "", fmt.Errorf(`%s has no SSH server: add "ghcr.io/devcontainers/features/sshd:1": {} to the devcontainer's features, then rebuild it: %w`, r.Codespace, ErrNoSSHServer)
	}
	if !res.OK() || res.Out == "" {
		return "", fmt.Errorf("gh codespace ssh --config failed: %s", strings.TrimSpace(res.Err))
	}
	if err := os.MkdirAll(r.cacheDir, 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(res.Out), 0o600)
}

var hostPattern = regexp.MustCompile(`(?m)^Host\s+(\S+)`)

// Host is the codespace's Host entry in the ssh config.
func (r *Remote) Host(ctx context.Context) (string, error) {
	if r.host != "" {
		return r.host, nil
	}
	path, err := r.ConfigPath(ctx)
	if err != nil {
		return "", err
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := hostPattern.FindStringSubmatch(string(text))
	if m == nil {
		return "", fmt.Errorf("no Host in %s", path)
	}
	r.host = m[1]
	return r.host, nil
}

// Forget drops the cached ssh config.
func (r *Remote) Forget() { os.Remove(r.cached()) }

// SSHCommand skips the local SSH agent (IdentityAgent=none), which adds
// about 60s to every new connection with 1Password before ssh falls back to
// the codespaces key, and shares one connection across runs.
func (r *Remote) SSHCommand(ctx context.Context) ([]string, error) {
	path, err := r.ConfigPath(ctx)
	if err != nil {
		return nil, err
	}
	return []string{"ssh", "-F", path,
		"-o", "IdentityAgent=none",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + r.home + "/.ssh/gh-csd-%C",
		"-o", "ControlPersist=15m"}, nil
}

// Capture runs script in the codespace with stdin and collects its output.
func (r *Remote) Capture(ctx context.Context, script, stdin string) (Result, error) {
	ssh, host, err := r.target(ctx)
	if err != nil {
		return Result{}, err
	}
	return r.runner.Capture(ctx, append(ssh, host, script), stdin)
}

// FileExists treats a codespace that is still starting as "not yet", so
// wait loops keep polling. One with no SSH server never gets there.
func (r *Remote) FileExists(ctx context.Context, path string) (bool, error) {
	res, err := r.Capture(ctx, "test -f "+path, "")
	if errors.Is(err, ErrNoSSHServer) || ctx.Err() != nil {
		return false, err
	}
	return err == nil && res.OK(), nil
}

// Run runs args in the codespace checkout, attached to this terminal; each
// argument reaches the remote shell as one word.
func (r *Remote) Run(ctx context.Context, args []string) (int, error) {
	ssh, host, err := r.target(ctx)
	if err != nil {
		return 0, err
	}
	if r.TTY {
		ssh = append(ssh, "-t")
	}
	script := "cd " + ShellEscape(r.Dir) + " && " + ShellJoin(args)
	return r.runner.Run(ctx, append(ssh, host, script))
}

// Copy rsyncs paths from fromDir to the codespace checkout.
func (r *Remote) Copy(ctx context.Context, paths []string, fromDir string) (Result, error) {
	if len(paths) == 0 {
		return Result{}, nil
	}
	shell, err := r.RsyncShell(ctx)
	if err != nil {
		return Result{}, err
	}
	host, err := r.Host(ctx)
	if err != nil {
		return Result{}, err
	}
	cmd := []string{"rsync", "-a", "--from0", "--files-from=-", "-e", shell, fromDir + "/", host + ":" + r.Dir + "/"}
	return r.runner.Capture(ctx, cmd, strings.Join(paths, "\x00")+"\x00")
}

// RsyncShell joins the ssh command with spaces: rsync splits -e on
// whitespace itself rather than through a shell, so it refuses words with
// whitespace.
func (r *Remote) RsyncShell(ctx context.Context) (string, error) {
	words, err := r.SSHCommand(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range words {
		if strings.ContainsAny(w, " \t\n") {
			return "", fmt.Errorf("rsync cannot pass %q to ssh: it contains whitespace", w)
		}
	}
	return strings.Join(words, " "), nil
}

func (r *Remote) target(ctx context.Context) ([]string, string, error) {
	ssh, err := r.SSHCommand(ctx)
	if err != nil {
		return nil, "", err
	}
	host, err := r.Host(ctx)
	return ssh, host, err
}

var shellUnsafe = regexp.MustCompile(`([^A-Za-z0-9_\-.,:+/@\n])`)

// ShellEscape quotes s as one word for sh, as Ruby's Shellwords.escape.
func ShellEscape(s string) string {
	if s == "" {
		return "''"
	}
	return strings.ReplaceAll(shellUnsafe.ReplaceAllString(s, `\$1`), "\n", "'\n'")
}

// ShellJoin quotes each argument with ShellEscape and joins them.
func ShellJoin(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = ShellEscape(a)
	}
	return strings.Join(words, " ")
}
