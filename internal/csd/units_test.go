package csd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- ChangeSet ---

func TestChangeSet(t *testing.T) {
	tests := []struct {
		name                          string
		changed, present, dirty       []string
		wantSync, wantGone, wantReset []string
	}{
		{"present files sync and missing ones are deleted",
			[]string{"b.rb", "a.rb", "gone.rb"}, []string{"a.rb", "b.rb"}, nil,
			[]string{"a.rb", "b.rb"}, []string{"gone.rb"}, nil},
		{"remote dirt outside the change set is reset",
			[]string{"a.rb"}, []string{"a.rb"}, []string{"a.rb", "reverted.rb", "stray.txt"},
			[]string{"a.rb"}, nil, []string{"reverted.rb", "stray.txt"}},
		{"remote dirt inside the change set is not reset",
			[]string{"a.rb", "gone.rb"}, []string{"a.rb"}, []string{"a.rb", "gone.rb"},
			[]string{"a.rb"}, []string{"gone.rb"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := NewChangeSet(tt.changed, tt.present, tt.dirty)
			equalSlice(t, tt.wantSync, set.Sync)
			equalSlice(t, tt.wantGone, set.Gone)
			equalSlice(t, tt.wantReset, set.Reset)
		})
	}
}

func TestSplitZAndParsePorcelain(t *testing.T) {
	equalSlice(t, []string{"a.rb", "dir/b c.rb", " lead.rb"}, SplitZ("a.rb\x00dir/b c.rb\x00a.rb\x00\x00 lead.rb\x00"))
	equalSlice(t, []string{"app/models/user.rb", "new file.rb", "old.rb"},
		ParsePorcelain(" M app/models/user.rb\x00?? new file.rb\x00 D old.rb\x00"))
}

// --- Config ---

func TestConfigPath(t *testing.T) {
	equal(t, "/x.yml", ConfigPath(map[string]string{"GH_CSD_CONFIG": "/x.yml", "XDG_CONFIG_HOME": "/xdg"}))
	equal(t, filepath.Join("/xdg", "gh-csd", "config.yml"), ConfigPath(map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}))
	equal(t, filepath.Join("/h", ".config", "gh-csd", "config.yml"), ConfigPath(map[string]string{"HOME": "/h"}))
}

func TestParseConfig(t *testing.T) {
	text := "machine: top\nready_check: none\nrepos:\n  me/my-app:\n    machine: mine\n  other/repo:\n    dir: /nope\n"
	got, err := ParseConfig(text, "c.yml", "me/my-app")
	must(t, err)
	equal(t, "mine", got["machine"])
	equal(t, "none", got["ready_check"])
	equal(t, "", got["dir"])
	equal(t, "", got["repos"])

	got, err = ParseConfig("default_permissions: true\nlist: [a]\nmap: {a: 1}\nempty:\n", "c.yml", "me/my-app")
	must(t, err)
	equal(t, "true", got["default_permissions"])
	equal(t, 1, len(got))
}

func TestEmptyMissingAndBadConfigs(t *testing.T) {
	got, err := ParseConfig("", "c.yml", "r")
	must(t, err)
	equal(t, 0, len(got))
	got, err = LoadConfig(map[string]string{"GH_CSD_CONFIG": "/no/such.yml"}, "r")
	must(t, err)
	equal(t, 0, len(got))
	_, err = ParseConfig("a: [", "c.yml", "r")
	mustFail(t, err, "c.yml is not valid YAML")
	_, err = ParseConfig("- a\n- b\n", "c.yml", "r")
	mustFail(t, err, "c.yml must be a mapping of settings")
}

func TestRepoFrom(t *testing.T) {
	alias := func(u *url.URL) *url.URL {
		if u.Hostname() == "github-work" {
			v := *u
			v.Host = "github.com"
			return &v
		}
		return u
	}
	tests := map[string]string{
		"git@github.com:me/repo.git":        "me/repo",
		"https://github.com/me/my.repo.git": "me/my.repo",
		"https://github.com/me/repo/":       "me/repo",
		"ssh://git@ssh.github.com:443/me/r": "me/r",
		"git@github-work:me/app.git":        "me/app",
		"git@gitlab.com:me/repo.git":        "",
		"https://github.com/me":             "",
		"not a url":                         "",
	}
	for remote, want := range tests {
		if got := RepoFrom(remote, alias); got != want {
			t.Errorf("RepoFrom(%q) = %q, want %q", remote, got, want)
		}
	}
	equal(t, "", RepoFrom("git@github-work:me/app.git", nil))
}

// --- Settings ---

func TestBuiltInDefaults(t *testing.T) {
	s := testSettings(nil, "/", map[string]string{}, nil)
	equal(t, ".devcontainer/devcontainer.json", s.Devcontainer())
	equal(t, "/workspaces/my-app", s.RemoteDir())
	equal(t, "", s.ReadyCheck())
	equal(t, "", s.Machine())
	equal(t, false, s.DefaultPermissions())
}

func TestFlagBeatsConfigBeatsDefault(t *testing.T) {
	config := map[string]string{"machine": "from-config"}
	equal(t, "flag", testSettings(nil, "/", config, map[string]string{"machine": "flag"}).Machine())
	equal(t, "from-config", testSettings(nil, "/", config, nil).Machine())
	s := testSettings(nil, "/", config, nil)
	s.SetMachine("chosen")
	equal(t, "chosen", s.Machine())
}

func TestReadyCheckAndFlagsOnly(t *testing.T) {
	equal(t, "", testSettings(nil, "/", map[string]string{"ready_check": "none"}, nil).ReadyCheck())
	equal(t, "pg_isready", testSettings(nil, "/", map[string]string{"ready_check": "pg_isready"}, nil).ReadyCheck())
	s := testSettings(nil, "/", nil, map[string]string{"codespace": "cs1", "takeover": "true"})
	equal(t, "cs1", s.Codespace())
	equal(t, true, s.Takeover())
	equal(t, false, testSettings(nil, "/", nil, nil).Takeover())
}

// Regression: yaml.v3 leaves YAML 1.1 booleans such as "no" as strings,
// and "no" turned default_permissions on.
func TestDefaultPermissionsParsesBooleansStrictly(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "yes": true, "On": true, "1": true, "false": false, "no": false, "off": false, "0": false} {
		s := testSettings(nil, "/", map[string]string{"default_permissions": value}, nil)
		must(t, s.Validate())
		if s.DefaultPermissions() != want {
			t.Errorf("default_permissions %q: want %v", value, want)
		}
	}
	err := testSettings(nil, "/", map[string]string{"default_permissions": "maybe"}, nil).Validate()
	mustFail(t, err, `default_permissions: want true or false, not "maybe"`)
}

func TestBranchFallsBackToOriginsDefaultThenMain(t *testing.T) {
	equal(t, "trunk", NewSettings(nil, "/", nil, map[string]string{}, "", "trunk").Branch())
	equal(t, "main", NewSettings(nil, "/", nil, map[string]string{}, "", "").Branch())
	equal(t, "cfg", NewSettings(nil, "/", nil, map[string]string{"branch": "cfg"}, "", "trunk").Branch())
}

func TestForCheckoutReadsTheOriginRemoteOrGHRepo(t *testing.T) {
	origin := func() *fakeRunner {
		return newFake().
			on(Result{Out: "git@github.com:me/repo.git\n"}, "git", "-C", "/src/x", "remote").
			on(Result{Out: "origin/trunk\n"}, "git", "-C", "/src/x", "symbolic-ref")
	}
	s, err := ForCheckout(bg, map[string]string{"GH_CSD_CONFIG": "/none.yml"}, "/src/x", nil, origin(), nil)
	must(t, err)
	equal(t, "me/repo", s.Repo())
	equal(t, "trunk", s.Branch())

	s, err = ForCheckout(bg, map[string]string{"GH_CSD_CONFIG": "/none.yml", "GH_REPO": "other/app"}, "/src/x", nil, origin(), nil)
	must(t, err)
	equal(t, "other/app", s.Repo())

	mustFail(t, NewSettings(nil, "/src/y", nil, nil, "", "").RequireRepo(), "no github.com origin remote in /src/y")
}

func TestForCheckoutRejectsABadBoolean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yml")
	write(t, path, "default_permissions: maybe\n")
	_, err := ForCheckout(bg, map[string]string{"GH_CSD_CONFIG": path}, "/src/x", nil, newFake(), nil)
	mustFail(t, err, "default_permissions")
}

// --- Wait ---

func TestWait(t *testing.T) {
	clock := &fakeClock{}
	results := []bool{false, false, true}
	ok, _ := Wait(bg, clock, time.Minute, 15*time.Second, func() (bool, error) { r := results[0]; results = results[1:]; return r, nil })
	equal(t, true, ok)
	equal(t, 2, len(clock.pauses))

	clock = &fakeClock{}
	checks := 0
	ok, _ = Wait(bg, clock, time.Minute, 15*time.Second, func() (bool, error) { checks++; return false, nil })
	equal(t, false, ok)
	equal(t, 5, checks) // at 0, 15, 30, 45, and 60
	equal(t, time.Minute, clock.now)
}

func TestWaitStopsOnAnErrorOrCancel(t *testing.T) {
	boom := errors.New("boom")
	_, err := Wait(bg, &fakeClock{}, time.Minute, time.Second, func() (bool, error) { return false, boom })
	equal(t, boom, err)

	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, err = Wait(ctx, &fakeClock{}, time.Minute, time.Second, func() (bool, error) { return false, nil })
	equal(t, true, errors.Is(err, context.Canceled))
}

// --- Remote ---

const sshConfig = "Host cs.fake-cs.master\n\tUser vscode\n"

func newTestRemote(t *testing.T, f *fakeRunner, home string) *Remote {
	s := testSettings(map[string]string{"HOME": home}, "/", nil, nil)
	s.CacheDir = filepath.Join(t.TempDir(), "cache")
	return NewRemote(f, "gh", "fake-cs", s, true)
}

func TestConfigIsCachedInAPrivateDirectory(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/Users/me")
	host, err := r.Host(bg)
	must(t, err)
	equal(t, "cs.fake-cs.master", host)
	info, err := os.Stat(r.cacheDir)
	must(t, err)
	if runtime.GOOS != "windows" { // Windows has no Unix permissions
		equal(t, os.FileMode(0o700), info.Mode().Perm())
	}

	again := &Remote{runner: f, gh: "gh", Codespace: "fake-cs", cacheDir: r.cacheDir}
	host, err = again.Host(bg)
	must(t, err)
	equal(t, "cs.fake-cs.master", host)
	equal(t, 1, len(f.matching("gh", "codespace", "ssh", "--config")))
}

func TestAFailedConfigFetchCachesNothing(t *testing.T) {
	f := newFake().on(Result{Err: "no such codespace\n", Status: 1}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/h")
	_, err := r.Host(bg)
	mustFail(t, err, "no such codespace")
	_, statErr := os.Stat(r.cached())
	equal(t, true, os.IsNotExist(statErr))
}

func TestFileExists(t *testing.T) {
	f := newFake().on(Result{Err: "codespace is starting\n", Status: 1}, "gh", "codespace", "ssh", "--config")
	ok, err := newTestRemote(t, f, "/h").FileExists(bg, "~/ready")
	must(t, err)
	equal(t, false, ok)

	f = newFake().on(Result{Err: "error getting ssh server details: failed to start SSH server: \n", Status: 1}, "gh", "codespace", "ssh", "--config")
	_, err = newTestRemote(t, f, "/h").FileExists(bg, "~/ready")
	equal(t, true, errors.Is(err, ErrNoSSHServer))
	mustFail(t, err, "features/sshd")
}

// Regression: without IdentityAgent=none, every new connection waited
// about 60s on the 1Password agent.
func TestEverySSHCallSkipsTheAgentAndSharesAConnection(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/Users/me")
	_, _ = r.Capture(bg, "true", "")
	_, _ = r.Run(bg, []string{"true"})
	_, _ = r.Copy(bg, []string{"a.rb"}, "/src")
	calls := f.matching("ssh")
	equal(t, 2, len(calls))
	for _, c := range calls {
		joined := strings.Join(c, " ")
		contains(t, joined, "IdentityAgent=none")
		contains(t, joined, "ControlMaster=auto")
		contains(t, joined, "ControlPath=/Users/me/.ssh/gh-csd-%C")
	}
	contains(t, f.matching("rsync")[0][5], "IdentityAgent=none")
}

func TestRunQuotesEachArgumentAndUsesATerminalOnlyWhenThereIsOne(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/h")
	_, err := r.Run(bg, []string{"bin/test", "test/a b.rb", "x;rm -rf /"})
	must(t, err)
	call := f.matching("ssh")[0]
	equal(t, `cd /workspaces/my-app && bin/test test/a\ b.rb x\;rm\ -rf\ /`, call[len(call)-1])
	equal(t, "-t", call[len(call)-3])

	r.TTY = false
	_, _ = r.Run(bg, []string{"true"})
	notContains(t, strings.Join(f.matching("ssh")[1], " "), " -t ")
}

func TestRunReportsAProgramThatCannotStart(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config").onStartError(fmt.Errorf("running ssh: not found"), "ssh")
	_, err := newTestRemote(t, f, "/h").Run(bg, []string{"true"})
	mustFail(t, err, "running ssh")
}

func TestCopy(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	_, err := newTestRemote(t, f, "/h").Copy(bg, []string{"a.rb", "dir/b c.rb"}, "/src")
	must(t, err)
	equal(t, "a.rb\x00dir/b c.rb\x00", f.stdinOf(func(c []string) bool { return c[0] == "rsync" }))
	call := f.matching("rsync")[0]
	equalSlice(t, []string{"/src/", "cs.fake-cs.master:/workspaces/my-app/"}, call[len(call)-2:])
	res, err := newTestRemote(t, newFake(), "/h").Copy(bg, nil, "/src")
	must(t, err)
	equal(t, true, res.OK())
}

func TestRsyncShellIsPlainWordsAndRefusesWhitespace(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	shell, err := newTestRemote(t, f, "/Users/me").RsyncShell(bg)
	must(t, err)
	contains(t, shell, "-o IdentityAgent=none -o ControlMaster=auto -o ControlPath=/Users/me/.ssh/gh-csd-%C -o ControlPersist=15m")
	f = newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	_, err = newTestRemote(t, f, "/Users/my name").RsyncShell(bg)
	mustFail(t, err, "contains whitespace")
}

func TestShellEscapeMatchesShellwords(t *testing.T) {
	equal(t, "''", ShellEscape(""))
	equal(t, `a\ b\'c`, ShellEscape("a b'c"))
	equal(t, "/workspaces/my-app", ShellEscape("/workspaces/my-app"))
}

// --- Codespaces ---

func newTestCodespaces(f *fakeRunner, a *fakeAPI, clock *fakeClock) (*Codespaces, *[]string) {
	opened := &[]string{}
	return &Codespaces{runner: f, api: a, gh: "gh", clock: clock, log: &strings.Builder{}, Open: func(url string) error {
		*opened = append(*opened, url)
		return nil
	}}, opened
}

func TestCreateTargetsTheBaseWithExplicitOptions(t *testing.T) {
	f := newFake().on(Result{Out: "Creating...\ncs-new\n"}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeAPI{}, &fakeClock{})
	name, err := c.Create(bg, strings.Repeat("é", 60), testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, "cs-new", name)
	cmd := strings.Join(f.matching("gh", "codespace", "create")[0], " ")
	contains(t, cmd, "-R me/my-app -b master -m test-machine --devcontainer-path .devcontainer/devcontainer.json -d "+strings.Repeat("é", 48))
	notContains(t, cmd, "--default-permissions")
}

func TestDefaultPermissionsOptsOutOfTheApproval(t *testing.T) {
	f := newFake().on(Result{Out: "cs\n"}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeAPI{}, &fakeClock{})
	_, err := c.Create(bg, "b", testSettings(nil, "/", nil, map[string]string{"default_permissions": "true"}))
	must(t, err)
	contains(t, strings.Join(f.matching("gh", "codespace", "create")[0], " "), "--default-permissions")
}

const approvalErr = "You must authorize or deny additional permissions requested by this codespace before continuing.\nOpen this URL in your browser to review and authorize additional permissions: https://github.com/me/my-app/codespaces/allow_permissions?ref=master\nAlternatively, you can run \"create\" with the \"--default-permissions\" option"

func TestApprovalOpensTheURLWaitsAndCreatesAgain(t *testing.T) {
	clock := &fakeClock{}
	f := newFake().
		on(Result{Err: approvalErr, Status: 1}, "gh", "codespace", "create").
		on(Result{Out: "cs-after\n"}, "gh", "codespace", "create")
	a := (&fakeAPI{}).
		on("GET", "repos/me/my-app/codespaces/permissions_check", 200, `{"accepted":false}`).
		on("GET", "repos/me/my-app/codespaces/permissions_check", 200, `{"accepted":true}`)
	c, opened := newTestCodespaces(f, a, clock)
	name, err := c.Create(bg, "b", testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, "cs-after", name)
	equalSlice(t, []string{"https://github.com/me/my-app/codespaces/allow_permissions?ref=master"}, *opened)
	equal(t, 5*time.Second, clock.now)
	contains(t, a.calls[0], "ref=master")
}

func TestNoApprovalWithinTenMinutesFails(t *testing.T) {
	clock := &fakeClock{}
	f := newFake().on(Result{Err: approvalErr, Status: 1}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeAPI{}, clock)
	_, err := c.Create(bg, "b", testSettings(nil, "/", nil, nil))
	mustFail(t, err, "--default-permissions")
	equal(t, 10*time.Minute, clock.now)
}

func TestOtherCreateFailuresReturnGhsError(t *testing.T) {
	f := newFake().on(Result{Err: "there is no such machine for the repository\n", Status: 1}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeAPI{}, &fakeClock{})
	_, err := c.Create(bg, "b", testSettings(nil, "/", nil, nil))
	mustFail(t, err, "no such machine")
}

// Regression: any failure of gh codespace view counted as "deleted", so a
// network error made down orphan the codespace and up create a second one.
func TestExistIsFalseOnlyOnANotFound(t *testing.T) {
	a := (&fakeAPI{}).on("GET", "user/codespaces/gone", 404, "").on("GET", "user/codespaces/flaky", 502, "")
	c, _ := newTestCodespaces(newFake(), a, &fakeClock{})
	ok, err := c.Exist(bg, "gone")
	must(t, err)
	equal(t, false, ok)
	_, err = c.Exist(bg, "flaky")
	mustFail(t, err, "checking codespace flaky")
	ok, err = c.Exist(bg, "here")
	must(t, err)
	equal(t, true, ok)
}

func TestMachinesAndDelete(t *testing.T) {
	a := (&fakeAPI{}).
		on("GET", "repos/me/my-app/codespaces/machines", 200, `{"machines":[{"name":"small"},{"name":"big"}]}`).
		on("DELETE", "user/codespaces/cs", 500, "")
	c, _ := newTestCodespaces(newFake(), a, &fakeClock{})
	machines, err := c.Machines(bg, testSettings(nil, "/", nil, nil))
	must(t, err)
	equalSlice(t, []string{"small", "big"}, machines)
	mustFail(t, c.Delete(bg, "cs"), "deleting codespace cs")
}

func TestStatesReadsEveryPage(t *testing.T) {
	var page1 []string
	for i := 0; i < pageSize; i++ {
		page1 = append(page1, fmt.Sprintf(`{"name":"cs-%d","state":"Available"}`, i))
	}
	a := (&fakeAPI{}).
		on("GET", "repos/me/my-app/codespaces?per_page=100&page=1", 200, `{"codespaces":[`+strings.Join(page1, ",")+`]}`).
		on("GET", "repos/me/my-app/codespaces?per_page=100&page=2", 200, `{"codespaces":[{"name":"last","state":"Shutdown"}]}`)
	c, _ := newTestCodespaces(newFake(), a, &fakeClock{})
	states, err := c.States(bg, testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, pageSize+1, len(states))
	equal(t, "Shutdown", states["last"])
	equal(t, 2, len(a.calls))
}

func TestApprovalStopsOnAClientError(t *testing.T) {
	f := newFake().on(Result{Err: approvalErr, Status: 1}, "gh", "codespace", "create")
	a := (&fakeAPI{}).on("GET", "repos/me/my-app/codespaces/permissions_check", 403, "")
	clock := &fakeClock{}
	c, _ := newTestCodespaces(f, a, clock)
	_, err := c.Create(bg, "b", testSettings(nil, "/", nil, nil))
	mustFail(t, err, "checking the permissions approval")
	equal(t, time.Duration(0), clock.now)
}

func TestGHRepoMustBeOnGitHubCom(t *testing.T) {
	_, err := ForCheckout(bg, map[string]string{"GH_CSD_CONFIG": "/none.yml", "GH_REPO": "ghe.example.com/o/r"}, "/src/x", nil, newFake(), nil)
	mustFail(t, err, "not ghe.example.com")
}

// --- ExecRunner ---

func TestExecRunner(t *testing.T) {
	res, err := ExecRunner{}.Capture(bg, child(t, "echo"), "in")
	must(t, err)
	equal(t, "in", res.Out)
	equal(t, "err\n", res.Err)
	equal(t, 3, res.Status)
	_, err = ExecRunner{}.Capture(bg, []string{filepath.Join(t.TempDir(), "missing")}, "")
	mustFail(t, err, "running ")
}

// Regression: a child killed by Ctrl-C reported exit -1, which run passed
// on as exit status 255 instead of gh's 2 for a cancel.
func TestExecRunnerReportsACancelNotTheKilledStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	code, err := ExecRunner{}.Run(ctx, child(t, "sleep"))
	equal(t, 0, code)
	equal(t, true, errors.Is(err, context.DeadlineExceeded))
	_, err = ExecRunner{}.Capture(ctx, child(t, "sleep"), "")
	equal(t, true, errors.Is(err, context.DeadlineExceeded))
}
