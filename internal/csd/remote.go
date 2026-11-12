package csd

import (
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
	Codespace string
	Dir       string
	cached    string
	home      string
	host      string
}

func NewRemote(r Runner, codespace string, s *Settings) *Remote {
	return &Remote{
		runner:    r,
		Codespace: codespace,
		Dir:       s.RemoteDir(),
		cached:    filepath.Join(s.TmpDir(), "gh-csd-"+codespace+".ssh"),
		home:      s.Home(),
	}
}

// ConfigPath is cached across runs: fetching it goes through gh and costs
// seconds.
func (r *Remote) ConfigPath() (string, error) {
	if info, err := os.Stat(r.cached); err == nil && info.Size() > 0 {
		return r.cached, nil
	}
	res := r.runner.Capture([]string{"gh", "codespace", "ssh", "--config", "-c", r.Codespace}, "")
	if strings.Contains(res.Err, "failed to start SSH server") {
		return "", fmt.Errorf(`%s has no SSH server: add "ghcr.io/devcontainers/features/sshd:1": {} to the devcontainer's features, then rebuild it: %w`, r.Codespace, ErrNoSSHServer)
	}
	if !res.OK() || res.Out == "" {
		return "", fmt.Errorf("gh codespace ssh --config failed: %s", strings.TrimSpace(res.Err))
	}
	return r.cached, os.WriteFile(r.cached, []byte(res.Out), 0o600)
}

var hostPattern = regexp.MustCompile(`(?m)^Host\s+(\S+)`)

func (r *Remote) Host() (string, error) {
	if r.host != "" {
		return r.host, nil
	}
	path, err := r.ConfigPath()
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
func (r *Remote) Forget() { os.Remove(r.cached) }

// SSHCommand skips the local SSH agent (IdentityAgent=none), which adds
// about 60s to every new connection with 1Password before ssh falls back to
// the codespaces key, and shares one connection across runs.
func (r *Remote) SSHCommand() ([]string, error) {
	path, err := r.ConfigPath()
	if err != nil {
		return nil, err
	}
	return []string{"ssh", "-F", path,
		"-o", "IdentityAgent=none",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + r.home + "/.ssh/gh-csd-%C",
		"-o", "ControlPersist=15m"}, nil
}

func (r *Remote) Capture(script, stdin string) (Result, error) {
	ssh, host, err := r.target()
	if err != nil {
		return Result{}, err
	}
	return r.runner.Capture(append(ssh, host, script), stdin), nil
}

// FileExists treats a codespace that is still starting as "not yet", so
// wait loops keep polling. One with no SSH server never gets there.
func (r *Remote) FileExists(path string) (bool, error) {
	res, err := r.Capture("test -f "+path, "")
	if errors.Is(err, ErrNoSSHServer) {
		return false, err
	}
	return err == nil && res.OK(), nil
}

// Run attaches args to the terminal; each argument reaches the remote shell
// as one word.
func (r *Remote) Run(args []string) (int, error) {
	ssh, host, err := r.target()
	if err != nil {
		return 0, err
	}
	script := "cd " + ShellEscape(r.Dir) + " && " + ShellJoin(args)
	return r.runner.Run(append(ssh, "-t", host, script)), nil
}

// Copy rsyncs paths from fromDir to the codespace checkout.
func (r *Remote) Copy(paths []string, fromDir string) (Result, error) {
	if len(paths) == 0 {
		return Result{}, nil
	}
	shell, err := r.RsyncShell()
	if err != nil {
		return Result{}, err
	}
	host, err := r.Host()
	if err != nil {
		return Result{}, err
	}
	cmd := []string{"rsync", "-a", "--from0", "--files-from=-", "-e", shell, fromDir + "/", host + ":" + r.Dir + "/"}
	return r.runner.Capture(cmd, strings.Join(paths, "\x00")+"\x00"), nil
}

// RsyncShell joins the ssh command with spaces: rsync splits -e on
// whitespace itself rather than through a shell, so it refuses words with
// whitespace.
func (r *Remote) RsyncShell() (string, error) {
	words, err := r.SSHCommand()
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

func (r *Remote) target() ([]string, string, error) {
	ssh, err := r.SSHCommand()
	if err != nil {
		return nil, "", err
	}
	host, err := r.Host()
	return ssh, host, err
}

var safeShell = regexp.MustCompile(`([^A-Za-z0-9_\-.,:+/@\n])`)

// ShellEscape quotes s as one word for sh, as Ruby's Shellwords.escape.
func ShellEscape(s string) string {
	if s == "" {
		return "''"
	}
	return strings.ReplaceAll(safeShell.ReplaceAllString(s, `\$1`), "\n", "'\n'")
}

func ShellJoin(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = ShellEscape(a)
	}
	return strings.Join(words, " ")
}
