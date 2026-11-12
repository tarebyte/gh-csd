package csd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- ChangeSet ---

func TestPresentFilesAreSyncedAndMissingOnesDeleted(t *testing.T) {
	set := NewChangeSet([]string{"b.rb", "a.rb", "gone.rb"}, []string{"a.rb", "b.rb"}, nil)
	equalSlice(t, []string{"a.rb", "b.rb"}, set.Sync)
	equalSlice(t, []string{"gone.rb"}, set.Gone)
}

func TestRemoteDirtOutsideTheChangeSetIsReset(t *testing.T) {
	set := NewChangeSet([]string{"a.rb"}, []string{"a.rb"}, []string{"a.rb", "reverted.rb", "stray.txt"})
	equalSlice(t, []string{"reverted.rb", "stray.txt"}, set.Reset)
}

func TestRemoteDirtInsideTheChangeSetIsNotReset(t *testing.T) {
	set := NewChangeSet([]string{"a.rb", "gone.rb"}, []string{"a.rb"}, []string{"a.rb", "gone.rb"})
	equal(t, 0, len(set.Reset))
}

func TestSplitZDropsEmptiesAndDuplicates(t *testing.T) {
	equalSlice(t, []string{"a.rb", "dir/b c.rb"}, SplitZ("a.rb\x00dir/b c.rb\x00a.rb\x00\x00"))
}

func TestParsePorcelainStripsStatusLetters(t *testing.T) {
	equalSlice(t, []string{"app/models/user.rb", "new file.rb", "old.rb"},
		ParsePorcelain(" M app/models/user.rb\x00?? new file.rb\x00 D old.rb\x00"))
}

// --- Config ---

func TestConfigPathPrefersGhCsdConfigThenXDGThenHome(t *testing.T) {
	equal(t, "/x.yml", ConfigPath(map[string]string{"GH_CSD_CONFIG": "/x.yml", "XDG_CONFIG_HOME": "/xdg"}))
	equal(t, "/xdg/gh-csd/config.yml", ConfigPath(map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}))
	equal(t, "/h/.config/gh-csd/config.yml", ConfigPath(map[string]string{"HOME": "/h"}))
}

func TestTheRepoSectionBeatsTheTopLevelAndOtherReposAreIgnored(t *testing.T) {
	text := "machine: top\nready_check: none\nrepos:\n  me/my-app:\n    machine: mine\n  other/repo:\n    dir: /nope\n"
	got, err := ParseConfig(text, "c.yml", "me/my-app")
	must(t, err)
	equal(t, "mine", got["machine"])
	equal(t, "none", got["ready_check"])
	equal(t, "", got["dir"])
	equal(t, "", got["repos"])
}

func TestValuesBecomeStringsAndNonScalarsAreDropped(t *testing.T) {
	got, err := ParseConfig("default_permissions: true\nlist: [a]\nmap: {a: 1}\nempty:\n", "c.yml", "me/my-app")
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

func TestRepoFromReadsSSHAndHTTPSOrigins(t *testing.T) {
	equal(t, "me/repo", RepoFrom("git@github.com:me/repo.git"))
	equal(t, "me/my.repo", RepoFrom("https://github.com/me/my.repo.git"))
	equal(t, "me/repo", RepoFrom("https://github.com/me/repo/"))
	equal(t, "", RepoFrom("git@gitlab.com:me/repo.git"))
}

// --- Settings ---

func TestBuiltInDefaults(t *testing.T) {
	s := testSettings(nil, "/", map[string]string{}, nil)
	equal(t, ".devcontainer/devcontainer.json", s.Devcontainer())
	equal(t, "/workspaces/my-app", s.RemoteDir())
	equal(t, "", s.ReadyCheck())
	equal(t, "", s.Machine())
	equal(t, false, s.DefaultPermissions())
	equal(t, "/tmp", s.TmpDir())
}

func TestFlagBeatsConfigBeatsDefault(t *testing.T) {
	config := map[string]string{"machine": "from-config"}
	equal(t, "flag", testSettings(nil, "/", config, map[string]string{"machine": "flag"}).Machine())
	equal(t, "from-config", testSettings(nil, "/", config, nil).Machine())
	s := testSettings(nil, "/", config, nil)
	s.SetMachine("chosen")
	equal(t, "chosen", s.Machine())
}

func TestReadyCheckNoneAndDefaultPermissions(t *testing.T) {
	equal(t, "", testSettings(nil, "/", map[string]string{"ready_check": "none"}, nil).ReadyCheck())
	equal(t, "pg_isready", testSettings(nil, "/", map[string]string{"ready_check": "pg_isready"}, nil).ReadyCheck())
	equal(t, true, testSettings(nil, "/", map[string]string{"default_permissions": "true"}, nil).DefaultPermissions())
	equal(t, false, testSettings(nil, "/", map[string]string{"default_permissions": "false"}, nil).DefaultPermissions())
	equal(t, true, testSettings(nil, "/", nil, map[string]string{"default_permissions": "true"}).DefaultPermissions())
}

func TestCodespaceAndTakeoverComeFromFlags(t *testing.T) {
	s := testSettings(nil, "/", nil, map[string]string{"codespace": "cs1", "takeover": "true"})
	equal(t, "cs1", s.Codespace())
	equal(t, true, s.Takeover())
	equal(t, false, testSettings(nil, "/", nil, nil).Takeover())
}

func TestBranchFallsBackToOriginsDefaultThenMain(t *testing.T) {
	equal(t, "trunk", NewSettings(nil, "/", nil, map[string]string{}, "", "trunk").Branch())
	equal(t, "main", NewSettings(nil, "/", nil, map[string]string{}, "", "").Branch())
	equal(t, "cfg", NewSettings(nil, "/", nil, map[string]string{"branch": "cfg"}, "", "trunk").Branch())
}

func TestForCheckoutReadsTheOriginRemote(t *testing.T) {
	f := newFake().
		on(Result{Out: "git@github.com:me/repo.git\n"}, "git", "-C", "/src/x", "remote").
		on(Result{Out: "origin/trunk\n"}, "git", "-C", "/src/x", "symbolic-ref")
	s, err := ForCheckout(map[string]string{"GH_CSD_CONFIG": "/none.yml"}, "/src/x", nil, f)
	must(t, err)
	equal(t, "me/repo", s.Repo())
	equal(t, "trunk", s.Branch())
	mustFail(t, NewSettings(nil, "/src/y", nil, nil, "", "").RequireRepo(), "no github.com origin remote in /src/y")
}

// --- Wait ---

func TestWaitStopsAtTheFirstSuccessAndGivesUpAfterAFinalCheck(t *testing.T) {
	clock := &fakeClock{}
	results := []bool{false, false, true}
	ok, _ := Wait(clock, time.Minute, 15*time.Second, func() (bool, error) { r := results[0]; results = results[1:]; return r, nil })
	equal(t, true, ok)
	equal(t, 2, len(clock.pauses))

	clock = &fakeClock{}
	checks := 0
	ok, _ = Wait(clock, time.Minute, 15*time.Second, func() (bool, error) { checks++; return false, nil })
	equal(t, false, ok)
	equal(t, 5, checks) // at 0, 15, 30, 45, and 60
	equal(t, time.Minute, clock.now)
}

func TestWaitStopsOnAnError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Wait(&fakeClock{}, time.Minute, time.Second, func() (bool, error) { return false, boom })
	equal(t, boom, err)
}

// --- Remote ---

const sshConfig = "Host cs.fake-cs.master\n\tUser vscode\n"

func newTestRemote(t *testing.T, f *fakeRunner, home string) *Remote {
	return NewRemote(f, "fake-cs", testSettings(map[string]string{"TMPDIR": t.TempDir(), "HOME": home}, "/", nil, nil))
}

func TestConfigIsFetchedOnceAndCached(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/Users/me")
	host, err := r.Host()
	must(t, err)
	equal(t, "cs.fake-cs.master", host)
	again := &Remote{runner: f, Codespace: "fake-cs", cached: r.cached}
	host, err = again.Host()
	must(t, err)
	equal(t, "cs.fake-cs.master", host)
	equal(t, 1, len(f.matching("gh", "codespace", "ssh", "--config")))
}

func TestAFailedConfigFetchRaisesAndCachesNothing(t *testing.T) {
	f := newFake().on(Result{Err: "no such codespace\n", Status: 1}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/h")
	_, err := r.Host()
	mustFail(t, err, "no such codespace")
	_, statErr := os.Stat(r.cached)
	equal(t, true, os.IsNotExist(statErr))
}

func TestAStillStartingCodespaceIsNotReadyYet(t *testing.T) {
	f := newFake().on(Result{Err: "codespace is starting\n", Status: 1}, "gh", "codespace", "ssh", "--config")
	ok, err := newTestRemote(t, f, "/h").FileExists("~/ready")
	must(t, err)
	equal(t, false, ok)
}

func TestACodespaceWithoutAnSSHServerFailsInsteadOfWaiting(t *testing.T) {
	f := newFake().on(Result{Err: "error getting ssh server details: failed to start SSH server: \n", Status: 1}, "gh", "codespace", "ssh", "--config")
	_, err := newTestRemote(t, f, "/h").FileExists("~/ready")
	equal(t, true, errors.Is(err, ErrNoSSHServer))
	mustFail(t, err, "features/sshd")
}

// Regression: without IdentityAgent=none, every new connection waited
// about 60s on the 1Password agent.
func TestEverySSHCallSkipsTheAgentAndSharesAConnection(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/Users/me")
	_, _ = r.Capture("true", "")
	_, _ = r.Run([]string{"true"})
	_, _ = r.Copy([]string{"a.rb"}, "/src")
	calls := f.matching("ssh")
	equal(t, 2, len(calls))
	for _, c := range calls {
		joined := strings.Join(c, " ")
		contains(t, joined, "IdentityAgent=none")
		contains(t, joined, "ControlMaster=auto")
		contains(t, joined, "ControlPath=/Users/me/.ssh/gh-csd-%C")
	}
	rsync := f.matching("rsync")[0]
	contains(t, rsync[5], "IdentityAgent=none")
}

func TestRunKeepsEachArgumentOneWordInTheRemoteShell(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	_, err := newTestRemote(t, f, "/h").Run([]string{"bin/test", "test/a b.rb", "x;rm -rf /"})
	must(t, err)
	call := f.matching("ssh")[0]
	equal(t, `cd /workspaces/my-app && bin/test test/a\ b.rb x\;rm\ -rf\ /`, call[len(call)-1])
	equal(t, "-t", call[len(call)-3])
}

func TestCopySendsANULSeparatedList(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	_, err := newTestRemote(t, f, "/h").Copy([]string{"a.rb", "dir/b c.rb"}, "/src")
	must(t, err)
	equal(t, "a.rb\x00dir/b c.rb\x00", f.stdinOf(func(c []string) bool { return c[0] == "rsync" }))
	call := f.matching("rsync")[0]
	equalSlice(t, []string{"/src/", "cs.fake-cs.master:/workspaces/my-app/"}, call[len(call)-2:])
	res, err := newTestRemote(t, newFake(), "/h").Copy(nil, "/src")
	must(t, err)
	equal(t, true, res.OK())
}

func TestRsyncShellIsPlainWordsAndRefusesWhitespace(t *testing.T) {
	f := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	r := newTestRemote(t, f, "/Users/me")
	shell, err := r.RsyncShell()
	must(t, err)
	contains(t, shell, "-o IdentityAgent=none -o ControlMaster=auto -o ControlPath=/Users/me/.ssh/gh-csd-%C -o ControlPersist=15m")
	f2 := newFake().on(Result{Out: sshConfig}, "gh", "codespace", "ssh", "--config")
	_, err = newTestRemote(t, f2, "/Users/my name").RsyncShell()
	mustFail(t, err, "contains whitespace")
}

func TestShellEscapeMatchesShellwords(t *testing.T) {
	equal(t, "''", ShellEscape(""))
	equal(t, `a\ b\'c`, ShellEscape("a b'c"))
	equal(t, "/workspaces/my-app", ShellEscape("/workspaces/my-app"))
}

// --- Codespaces ---

func newTestCodespaces(f *fakeRunner, clock *fakeClock) (*Codespaces, *[]string) {
	opened := &[]string{}
	return &Codespaces{runner: f, clock: clock, log: &strings.Builder{}, Open: func(url string) error {
		*opened = append(*opened, url)
		return nil
	}}, opened
}

func TestCreateTargetsTheBaseWithExplicitOptions(t *testing.T) {
	f := newFake().on(Result{Out: "Creating...\ncs-new\n"}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeClock{})
	name, err := c.Create(strings.Repeat("x", 60), testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, "cs-new", name)
	cmd := strings.Join(f.matching("gh", "codespace", "create")[0], " ")
	contains(t, cmd, "-R me/my-app -b master -m test-machine --devcontainer-path .devcontainer/devcontainer.json -d "+strings.Repeat("x", 48))
	notContains(t, cmd, "--default-permissions")
}

func TestDefaultPermissionsOptsOutOfTheApproval(t *testing.T) {
	f := newFake().on(Result{Out: "cs\n"}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeClock{})
	_, err := c.Create("b", testSettings(nil, "/", nil, map[string]string{"default_permissions": "true"}))
	must(t, err)
	contains(t, strings.Join(f.matching("gh", "codespace", "create")[0], " "), "--default-permissions")
}

const approvalErr = "You must authorize or deny additional permissions requested by this codespace before continuing.\nOpen this URL in your browser to review and authorize additional permissions: https://github.com/me/my-app/codespaces/allow_permissions?ref=master\nAlternatively, you can run \"create\" with the \"--default-permissions\" option"

func TestApprovalOpensTheURLWaitsAndCreatesAgain(t *testing.T) {
	clock := &fakeClock{}
	f := newFake().
		on(Result{Err: approvalErr, Status: 1}, "gh", "codespace", "create").
		on(Result{Out: "false\n"}, "gh", "api").
		on(Result{Out: "true\n"}, "gh", "api").
		on(Result{Out: "cs-after\n"}, "gh", "codespace", "create")
	c, opened := newTestCodespaces(f, clock)
	name, err := c.Create("b", testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, "cs-after", name)
	equalSlice(t, []string{"https://github.com/me/my-app/codespaces/allow_permissions?ref=master"}, *opened)
	equal(t, 5*time.Second, clock.now)
}

func TestNoApprovalWithinTenMinutesFails(t *testing.T) {
	clock := &fakeClock{}
	f := newFake().on(Result{Err: approvalErr, Status: 1}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, clock)
	_, err := c.Create("b", testSettings(nil, "/", nil, nil))
	mustFail(t, err, "--default-permissions")
	equal(t, 10*time.Minute, clock.now)
}

func TestOtherCreateFailuresReturnGhsError(t *testing.T) {
	f := newFake().on(Result{Err: "there is no such machine for the repository\n", Status: 1}, "gh", "codespace", "create")
	c, _ := newTestCodespaces(f, &fakeClock{})
	_, err := c.Create("b", testSettings(nil, "/", nil, nil))
	mustFail(t, err, "no such machine")
}

func TestMachinesAndStates(t *testing.T) {
	f := newFake().
		on(Result{Out: "small\n\nbig\n"}, "gh", "api").
		on(Result{Out: "cs-a Available\ncs-b Shutdown\n"}, "gh", "codespace", "list")
	c, _ := newTestCodespaces(f, &fakeClock{})
	machines, err := c.Machines(testSettings(nil, "/", nil, nil))
	must(t, err)
	equalSlice(t, []string{"small", "big"}, machines)
	equalSlice(t, []string{"gh", "api", "repos/me/my-app/codespaces/machines", "--jq", ".machines[].name"}, f.calls[0])
	states, err := c.States(testSettings(nil, "/", nil, nil))
	must(t, err)
	equal(t, "Available", states["cs-a"])
	equal(t, "Shutdown", states["cs-b"])
}

func TestExistAndDelete(t *testing.T) {
	f := newFake().on(Result{Status: 1}, "gh", "codespace", "view").on(Result{Err: "nope", Status: 1}, "gh", "codespace", "delete")
	c, _ := newTestCodespaces(f, &fakeClock{})
	equal(t, false, c.Exist("cs"))
	equal(t, true, c.Exist("cs"))
	mustFail(t, c.Delete("cs"), "gh codespace delete failed: nope")
}

// --- ExecRunner ---

func TestExecRunnerCapturesOutputAndStatus(t *testing.T) {
	res := ExecRunner{}.Capture([]string{"sh", "-c", "cat; echo err >&2; exit 3"}, "in")
	equal(t, "in", res.Out)
	equal(t, "err\n", res.Err)
	equal(t, 3, res.Status)
	equal(t, 127, ExecRunner{}.Capture([]string{filepath.Join(t.TempDir(), "missing")}, "").Status)
}
