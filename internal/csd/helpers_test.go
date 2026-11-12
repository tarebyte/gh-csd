package csd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// bg is the context for tests that never cancel.
var bg = context.Background()

// fakeRunner stands in for ExecRunner: it records every command and
// replies with scripted results. Unscripted commands succeed with empty
// output, except passthrough programs (git), which run for real.
type fakeRunner struct {
	calls   [][]string
	stdins  []string
	replies []reply
	pass    map[string]bool
}

type reply struct {
	match func([]string) bool
	res   Result
	err   error
}

func newFake(passthrough ...string) *fakeRunner {
	f := &fakeRunner{pass: map[string]bool{}}
	for _, p := range passthrough {
		f.pass[p] = true
	}
	return f
}

// on scripts the reply to the next command starting with prefix. Replies
// are used once each, in the order they were added.
func (f *fakeRunner) on(res Result, prefix ...string) *fakeRunner {
	f.replies = append(f.replies, reply{match: func(cmd []string) bool { return hasPrefix(cmd, prefix) }, res: res})
	return f
}

// onSSH scripts the reply to the next ssh call whose script contains
// fragment.
func (f *fakeRunner) onSSH(fragment string, res Result) *fakeRunner {
	f.replies = append(f.replies, reply{match: func(cmd []string) bool {
		return cmd[0] == "ssh" && strings.Contains(cmd[len(cmd)-1], fragment)
	}, res: res})
	return f
}

// onStartError scripts a program that cannot start.
func (f *fakeRunner) onStartError(err error, prefix ...string) *fakeRunner {
	f.replies = append(f.replies, reply{func(cmd []string) bool { return hasPrefix(cmd, prefix) }, Result{}, err})
	return f
}

func (f *fakeRunner) Capture(ctx context.Context, cmd []string, stdin string) (Result, error) {
	f.calls = append(f.calls, cmd)
	f.stdins = append(f.stdins, stdin)
	if f.pass[cmd[0]] {
		return ExecRunner{}.Capture(ctx, cmd, stdin)
	}
	return f.replyFor(cmd)
}

func (f *fakeRunner) Run(_ context.Context, cmd []string) (int, error) {
	f.calls = append(f.calls, cmd)
	f.stdins = append(f.stdins, "")
	res, err := f.replyFor(cmd)
	return res.Status, err
}

func (f *fakeRunner) replyFor(cmd []string) (Result, error) {
	for i, r := range f.replies {
		if r.match(cmd) {
			f.replies = append(f.replies[:i], f.replies[i+1:]...)
			return r.res, r.err
		}
	}
	return Result{}, nil
}

// fakeAPI stands in for go-gh's REST client. Unscripted requests succeed
// with an empty object.
type fakeAPI struct {
	calls   []string
	replies []apiReply
}

type apiReply struct {
	method, path string
	status       int
	body         string
}

// on scripts the reply to the next request whose path starts with path.
func (a *fakeAPI) on(method, path string, status int, body string) *fakeAPI {
	a.replies = append(a.replies, apiReply{method, path, status, body})
	return a
}

func (a *fakeAPI) DoWithContext(_ context.Context, method, path string, _ io.Reader, response any) error {
	a.calls = append(a.calls, method+" "+path)
	for i, r := range a.replies {
		if r.method == method && strings.HasPrefix(path, r.path) {
			a.replies = append(a.replies[:i], a.replies[i+1:]...)
			if r.status >= 400 {
				return &api.HTTPError{StatusCode: r.status, Message: http.StatusText(r.status)}
			}
			if response != nil && r.body != "" {
				return json.Unmarshal([]byte(r.body), response)
			}
			return nil
		}
	}
	return nil
}

func (f *fakeRunner) matching(prefix ...string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if hasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeRunner) sshScripts() []string {
	var out []string
	for _, c := range f.matching("ssh") {
		out = append(out, c[len(c)-1])
	}
	return out
}

func (f *fakeRunner) stdinOf(match func([]string) bool) string {
	for i, c := range f.calls {
		if match(c) {
			return f.stdins[i]
		}
	}
	return ""
}

func hasPrefix(cmd, prefix []string) bool {
	if len(cmd) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if cmd[i] != p {
			return false
		}
	}
	return true
}

func countOf(items []string, s string) int {
	n := 0
	for _, item := range items {
		if item == s {
			n++
		}
	}
	return n
}

// TestMain lets the test binary stand in for a child program on every
// platform (Go's helper-process pattern): with CSD_TEST_CHILD set, it acts
// as that program instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("CSD_TEST_CHILD") {
	case "":
		os.Exit(m.Run())
	case "echo":
		in, _ := io.ReadAll(os.Stdin)
		os.Stdout.Write(in)
		os.Stderr.WriteString("err\n")
		os.Exit(3)
	case "sleep":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
}

// child is a command that runs this test binary as the named program.
func child(t *testing.T, name string) []string {
	t.Setenv("CSD_TEST_CHILD", name)
	return []string{os.Args[0]}
}

// fakeClock advances instantly.
type fakeClock struct {
	now    time.Duration
	pauses []time.Duration
}

func (c *fakeClock) Now() time.Duration { return c.now }
func (c *fakeClock) Pause(ctx context.Context, d time.Duration) error {
	c.pauses = append(c.pauses, d)
	c.now += d
	return ctx.Err()
}

// fakePrompt answers with scripted choices.
type fakePrompt struct {
	interactive bool
	choice      int
	confirm     bool
	asked       []string
}

func (p *fakePrompt) Interactive() bool { return p.interactive }
func (p *fakePrompt) Choose(q string, options []string) (string, error) {
	p.asked = append(p.asked, q)
	return options[p.choice], nil
}
func (p *fakePrompt) Confirm(q string) (bool, error) {
	p.asked = append(p.asked, q)
	return p.confirm, nil
}

// testSettings are settings for the fake origin me/my-app, whose default
// branch is master. They never read the real config file.
func testSettings(env map[string]string, cwd string, config, flags map[string]string) *Settings {
	if config == nil {
		config = map[string]string{"machine": "test-machine"}
	}
	return NewSettings(env, cwd, flags, config, "me/my-app", "master")
}

// gitRepo makes a throwaway origin and clone (main checkout at
// root/my-app), ignoring the user's git config so signing and hooks never
// run.
func gitRepo(t *testing.T) (root, main string) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// git prints forward slashes on Windows too.
	root = filepath.ToSlash(root)
	main = root + "/my-app"
	sh(t, "git", "init", "-q", "--bare", "-b", "master", root+"/origin.git")
	sh(t, "git", "clone", "-q", root+"/origin.git", main)
	sh(t, "git", "-C", main, "commit", "-q", "--allow-empty", "-m", "init")
	sh(t, "git", "-C", main, "push", "-q", "origin", "HEAD:master")
	return root, main
}

func sh(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	c := exec.Command(args[0], args[1:]...)
	c.Stdout, c.Stderr = &out, &errOut
	if err := c.Run(); err != nil {
		t.Fatalf("%s: %v: %s", strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

// bound is w's codespace binding, failing the test on a read error.
func bound(t *testing.T, w *Worktree) string {
	t.Helper()
	name, err := w.Codespace(bg)
	must(t, err)
	return name
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equal[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("want %v, got %v", want, got)
	}
}

func equalSlice(t *testing.T, want, got []string) {
	t.Helper()
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") || len(want) != len(got) {
		t.Errorf("want %q, got %q", want, got)
	}
}

func contains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%q does not contain %q", haystack, needle)
	}
}

func notContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%q contains %q", haystack, needle)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error containing %q, got none", fragment)
	}
	contains(t, err.Error(), fragment)
}
