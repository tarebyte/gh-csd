package csd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	cmdSSHConfig = "Host cs.test.master\n"
	readyCheck   = "nc -z localhost 5432"
)

// env is one throwaway repo with gh, ssh, and rsync faked and git real.
type env struct {
	t          *testing.T
	root, main string
	fake       *fakeRunner
	api        *fakeAPI
	clock      *fakeClock
	prompt     *fakePrompt
	out, err   strings.Builder
	opened     []string
}

func newEnv(t *testing.T) *env {
	root, main := gitRepo(t)
	return &env{t: t, root: root, main: main, fake: newFake("git"), api: &fakeAPI{}, clock: &fakeClock{}, prompt: &fakePrompt{}}
}

func (e *env) commands(cwd string, config, flags map[string]string) *Commands {
	cfg := map[string]string{"machine": "test-machine", "ready_check": readyCheck}
	for k, v := range config {
		cfg[k] = v
	}
	s := testSettings(map[string]string{"HOME": "/Users/me"}, cwd, cfg, flags)
	s.CacheDir = e.root + "/cache"
	return NewCommands(s, Deps{Runner: e.fake, API: e.api, GH: "gh", Clock: e.clock, Prompt: e.prompt,
		IO:   IO{Out: &e.out, Err: &e.err, Width: 80, StdinTTY: true},
		Open: func(url string) error { e.opened = append(e.opened, url); return nil }})
}

func (e *env) at(cwd string) *Commands { return e.commands(cwd, nil, nil) }

func (e *env) tree(branch string) *Worktree {
	w, err := ForBranch(bg, ExecRunner{}, branch, e.main, "master")
	must(e.t, err)
	return w
}

func (e *env) created(branch string) *Worktree {
	w := e.tree(branch)
	_, err := w.Create(bg)
	must(e.t, err)
	return w
}

func (e *env) newCodespace() *env {
	e.fake.on(Result{Out: "cs-new\n"}, "gh", "codespace", "create").
		on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config")
	return e
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- worktree ---

func TestPathSitsNextToTheMainCheckoutWithDashesForSlashes(t *testing.T) {
	e := newEnv(t)
	w := e.tree("me/feature")
	equal(t, e.root+"/my-app-me-feature", w.Path)
	equal(t, e.main, w.Main)
	first := e.created("me/one")
	w, err := ForBranch(bg, ExecRunner{}, "me/two", first.Path, "master")
	must(t, err)
	equal(t, e.root+"/my-app-me-two", w.Path)
}

func TestReusesAWorktreeAnotherToolMadeForTheBranch(t *testing.T) {
	e := newEnv(t)
	elsewhere := e.root + "/elsewhere/my-app/me-feature"
	sh(t, "git", "-C", e.main, "worktree", "add", "-q", "-b", "me/feature", elsewhere, "origin/master")
	w := e.tree("me/feature")
	equal(t, elsewhere, w.Path)
	created, err := w.Create(bg)
	must(t, err)
	equal(t, false, created)
	must(t, w.Remove(bg, false))
	equal(t, false, exists(elsewhere))
}

func TestNeverPicksTheMainCheckout(t *testing.T) {
	e := newEnv(t)
	equal(t, e.root+"/my-app-master", e.tree("master").Path)
}

func TestCreateBranchesFromTheBaseOrChecksOutAnExistingBranch(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	equal(t, "me/feature", sh(t, "git", "-C", w.Path, "branch", "--show-current"))
	again, err := w.Create(bg)
	must(t, err)
	equal(t, false, again)

	sh(t, "git", "-C", e.main, "branch", "me/existing")
	equal(t, "me/existing", sh(t, "git", "-C", e.created("me/existing").Path, "branch", "--show-current"))
}

func TestBindingLivesInTheWorktreesOwnGitDir(t *testing.T) {
	e := newEnv(t)
	one, two := e.created("me/one"), e.created("me/two")
	equal(t, "", bound(t, one))
	must(t, one.Bind(bg, "cs-one"))
	must(t, two.Bind(bg, "cs-two"))
	equal(t, "cs-one", bound(t, one))
	cur, err := Current(bg, ExecRunner{}, two.Path, "master")
	must(t, err)
	equal(t, "cs-two", bound(t, cur))
	equal(t, true, exists(e.main+"/.git/worktrees/my-app-me-one/"+BindingFile))
}

func TestUnsavedWorkListsUncommittedAndUnpushedWork(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	work, err := w.UnsavedWork(bg)
	must(t, err)
	equal(t, "", work)
	write(t, w.Path+"/new.txt", "x")
	work, _ = w.UnsavedWork(bg)
	contains(t, work, "uncommitted:\n?? new.txt")
	sh(t, "git", "-C", w.Path, "add", "new.txt")
	sh(t, "git", "-C", w.Path, "commit", "-q", "-m", "wip")
	work, _ = w.UnsavedWork(bg)
	contains(t, work, "unpushed:\n")
	sh(t, "git", "-C", w.Path, "push", "-q", "origin", "me/feature")
	work, _ = w.UnsavedWork(bg)
	equal(t, "", work)
}

func TestChangedPathsListsDiffsUntrackedAndDeletedPaths(t *testing.T) {
	e := newEnv(t)
	write(t, e.main+"/kept.rb", "1")
	write(t, e.main+"/doomed.rb", "1")
	sh(t, "git", "-C", e.main, "add", ".")
	sh(t, "git", "-C", e.main, "commit", "-q", "-m", "files")
	sh(t, "git", "-C", e.main, "push", "-q", "origin", "HEAD:master")
	w := e.created("me/feature")
	write(t, w.Path+"/kept.rb", "2")
	must(t, os.Remove(w.Path+"/doomed.rb"))
	write(t, w.Path+"/new file.rb", "3")
	changed, err := w.ChangedPaths(bg)
	must(t, err)
	equalSlice(t, []string{"doomed.rb", "kept.rb", "new file.rb"}, sorted(changed))
	equalSlice(t, []string{"kept.rb", "new file.rb"}, sorted(w.Existing(changed)))
}

// --- up ---

func TestUpCreatesTheWorktreeAndCodespaceThenWaitsAndSyncs(t *testing.T) {
	e := newEnv(t).newCodespace()
	e.fake.onSSH("test -f ~/.local/state/gh-csd/setup-ready", Result{Status: 1})
	must(t, e.at(e.main).Up(bg, "me/feature", false))
	w := e.tree("me/feature")
	equal(t, true, w.Exist())
	equal(t, "cs-new", bound(t, w))
	equal(t, 15*time.Second, e.clock.now)
	equal(t, "", w.Phase(bg))
	for _, s := range e.fake.sshScripts() {
		notContains(t, s, "setup-complete")
	}
	host, _ := os.Hostname()
	contains(t, strings.Join(e.fake.sshScripts(), "\n"), "echo "+ShellEscape(strings.SplitN(host, ".", 2)[0]+":"+w.Path)+" > /workspaces/my-app/.git/cs-dev-owner")
	contains(t, e.out.String(), "gh csd: ready  worktree="+w.Path+"  codespace=cs-new")
	contains(t, e.err.String(), "copied 0, deleted 0, reset 0")
}

func TestUpClearsThePhaseWhenCreateFails(t *testing.T) {
	e := newEnv(t)
	e.fake.on(Result{Err: "there is no such machine\n", Status: 1}, "gh", "codespace", "create")
	mustFail(t, e.at(e.main).Up(bg, "me/feature", false), "no such machine")
	equal(t, "", e.tree("me/feature").Phase(bg))
}

func TestUpReusesABoundCodespaceThatStillExists(t *testing.T) {
	e := newEnv(t)
	must(t, e.created("me/feature").Bind(bg, "cs-old"))
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config")
	must(t, e.at(e.main).Up(bg, "me/feature", false))
	contains(t, e.err.String(), "reusing worktree")
	contains(t, e.err.String(), "reusing codespace cs-old")
	equal(t, 0, len(e.fake.matching("gh", "codespace", "create")))
}

func TestUpReplacesABoundCodespaceThatIsGone(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs-gone"))
	e.api.on("GET", "user/codespaces/cs-gone", 404, "")
	e.newCodespace()
	must(t, e.at(e.main).Up(bg, "me/feature", false))
	equal(t, "cs-new", bound(t, w))
}

// Regression: the first test after up failed on a database, which accepts
// connections a while after the ready marker.
func TestUpWaitsForTheServicesAfterTheReadyMarker(t *testing.T) {
	e := newEnv(t).newCodespace()
	e.fake.onSSH(readyCheck, Result{Status: 1}).onSSH(readyCheck, Result{Status: 1})
	must(t, e.at(e.main).Up(bg, "me/feature", false))
	contains(t, e.err.String(), "waiting for services...")
	equal(t, 10*time.Second, e.clock.now)
	equal(t, 3, countOf(e.fake.sshScripts(), readyCheck))
}

func TestUpWithReadyCheckNoneSkipsTheServicesWait(t *testing.T) {
	e := newEnv(t).newCodespace()
	must(t, e.commands(e.main, map[string]string{"ready_check": "none"}, nil).Up(bg, "me/feature", false))
	notContains(t, e.err.String(), "waiting for services")
}

func TestUpContinuesWhenTheServicesNeverComeUp(t *testing.T) {
	e := newEnv(t).newCodespace()
	for i := 0; i < 61; i++ {
		e.fake.onSSH("nc -z", Result{Status: 1})
	}
	must(t, e.at(e.main).Up(bg, "me/feature", false))
	contains(t, e.err.String(), "services not ready after 5m; continuing")
	contains(t, e.out.String(), "gh csd: ready")
}

func TestUpPassesSettingsToCreate(t *testing.T) {
	e := newEnv(t).newCodespace()
	must(t, e.commands(e.main, nil, map[string]string{"machine": "big-machine", "default_permissions": "true"}).Up(bg, "me/feature", false))
	cmd := strings.Join(e.fake.matching("gh", "codespace", "create")[0], " ")
	contains(t, cmd, "-m big-machine")
	contains(t, cmd, "--default-permissions")
}

func TestUpWithoutABranchUsesTheWorktreeItRunsIn(t *testing.T) {
	e := newEnv(t).newCodespace()
	elsewhere := e.root + "/elsewhere/me-feature"
	sh(t, "git", "-C", e.main, "worktree", "add", "-q", "-b", "me/feature", elsewhere, "origin/master")
	must(t, e.at(elsewhere).Up(bg, "", false))
	contains(t, e.err.String(), "reusing worktree "+elsewhere)
	contains(t, strings.Join(e.fake.matching("gh", "codespace", "create")[0], " "), "-d me/feature")
	cur, _ := Current(bg, ExecRunner{}, elsewhere, "master")
	equal(t, "cs-new", bound(t, cur))
}

func TestUpWithoutABranchRefusesTheMainCheckout(t *testing.T) {
	e := newEnv(t)
	mustFail(t, e.at(e.main).Up(bg, "", false), "main checkout")
}

func TestUpPicksTheOnlyMachineTypeWithoutAsking(t *testing.T) {
	e := newEnv(t)
	e.api.on("GET", "repos/me/my-app/codespaces/machines", 200, `{"machines":[{"name":"only-one"}]}`)
	e.newCodespace()
	must(t, e.commands(e.main, map[string]string{"machine": ""}, nil).Up(bg, "me/feature", false))
	contains(t, strings.Join(e.fake.matching("gh", "codespace", "create")[0], " "), "-m only-one")
}

func TestUpAsksForTheMachineTypeInATerminal(t *testing.T) {
	e := newEnv(t)
	e.prompt.interactive, e.prompt.choice = true, 1
	e.api.on("GET", "repos/me/my-app/codespaces/machines", 200, `{"machines":[{"name":"small"},{"name":"big"}]}`)
	e.newCodespace()
	must(t, e.commands(e.main, map[string]string{"machine": ""}, nil).Up(bg, "me/feature", false))
	contains(t, strings.Join(e.fake.matching("gh", "codespace", "create")[0], " "), "-m big")
}

func TestUpWithoutATerminalNamesTheMachineTypes(t *testing.T) {
	e := newEnv(t)
	e.api.on("GET", "repos/me/my-app/codespaces/machines", 200, `{"machines":[{"name":"small"},{"name":"big"}]}`)
	err := e.commands(e.main, map[string]string{"machine": ""}, nil).Up(bg, "me/feature", false)
	mustFail(t, err, "pass -m with one of: small, big")
	equal(t, 0, len(e.fake.matching("gh", "codespace", "create")))
}

func TestUpStatusWaitsForTheDotfilesToFinish(t *testing.T) {
	e := newEnv(t).newCodespace()
	e.fake.onSSH("setup-complete", Result{Status: 1})
	must(t, e.commands(e.main, map[string]string{"ready_check": "none"}, nil).Up(bg, "me/feature", true))
	contains(t, e.err.String(), "waiting for the dotfiles to finish...")
	contains(t, e.err.String(), "dotfiles finished")
	equal(t, 30*time.Second, e.clock.now)
	equal(t, "", e.tree("me/feature").Phase(bg))
}

func TestUpStopsAtOnceWithoutAnSSHServer(t *testing.T) {
	e := newEnv(t)
	e.fake.on(Result{Out: "cs-new\n"}, "gh", "codespace", "create").
		on(Result{Err: "failed to start SSH server", Status: 1}, "gh", "codespace", "ssh", "--config")
	mustFail(t, e.at(e.main).Up(bg, "me/feature", false), "has no SSH server")
	equal(t, time.Duration(0), e.clock.now)
}

// --- run ---

func TestRunCopiesDeletesAndResetsThenRunsTheCommand(t *testing.T) {
	e := newEnv(t)
	write(t, e.main+"/doomed.rb", "1")
	sh(t, "git", "-C", e.main, "add", ".")
	sh(t, "git", "-C", e.main, "commit", "-q", "-m", "doomed")
	sh(t, "git", "-C", e.main, "push", "-q", "origin", "HEAD:master")
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	must(t, os.Remove(w.Path+"/doomed.rb"))
	write(t, w.Path+"/new.rb", "2")
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").
		onSSH("git status --porcelain", Result{Out: " M stray.rb\x00 D doomed.rb\x00"}).
		onSSH("bin/test", Result{Status: 3})

	code, err := e.at(w.Path).Run(bg, []string{"bin/test", "test/x.rb"})
	must(t, err)
	equal(t, 3, code)
	contains(t, e.err.String(), "copied 1, deleted 1, reset 1")
	equal(t, "new.rb\x00", e.fake.stdinOf(func(c []string) bool { return c[0] == "rsync" }))
	rsync := e.fake.matching("rsync")[0]
	equal(t, w.Path+"/", rsync[len(rsync)-2])
	equal(t, "doomed.rb\x00", e.fake.stdinOf(func(c []string) bool { return c[0] == "ssh" && strings.Contains(c[len(c)-1], "rm -f") }))
	equal(t, "stray.rb\x00", e.fake.stdinOf(func(c []string) bool { return c[0] == "ssh" && strings.Contains(c[len(c)-1], "git cat-file") }))
}

func TestRunWithNoCommandOnlySyncs(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config")
	code, err := e.at(w.Path).Run(bg, nil)
	must(t, err)
	equal(t, 0, code)
	for _, c := range e.fake.matching("ssh") {
		notContains(t, strings.Join(c, " "), " -t ")
	}
}

// Regression: the shell version saved the binding before the owner check,
// so a refused run still rebound the worktree.
func TestRunRefusesACodespaceAnotherWorktreeOwnsAndKeepsTheBinding(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").onSSH("cat ", Result{Out: "other-mac:/src/other\n"})
	_, err := e.commands(w.Path, nil, map[string]string{"codespace": "cs-taken"}).Run(bg, nil)
	mustFail(t, err, "in use by other-mac:/src/other; use another codespace, or pass --takeover")
	equal(t, "", bound(t, w))
	equal(t, 0, len(e.fake.matching("rsync")))
}

func TestRunTakeoverClaimsTheCodespace(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").onSSH("cat ", Result{Out: "other-mac:/src/other\n"})
	_, err := e.commands(w.Path, nil, map[string]string{"codespace": "cs-taken", "takeover": "true"}).Run(bg, nil)
	must(t, err)
	equal(t, "cs-taken", bound(t, w))
}

func TestRunWithoutACodespaceSaysToRunUp(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	_, err := e.at(w.Path).Run(bg, nil)
	mustFail(t, err, "gh csd up")
}

func TestRunStopsWhenRsyncFails(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	write(t, w.Path+"/new.rb", "1")
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").on(Result{Err: "connection closed", Status: 12}, "rsync")
	_, err := e.at(w.Path).Run(bg, []string{"true"})
	mustFail(t, err, "rsync failed")
}

// --- down ---

func TestDownRefusesToLoseWork(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	write(t, w.Path+"/new.rb", "1")
	mustFail(t, e.at(e.main).Down(bg, "me/feature", false), "uncommitted:\n?? new.rb")
	equal(t, true, w.Exist())
	notContains(t, strings.Join(e.api.calls, "\n"), "DELETE")
}

func TestDownDeletesTheCodespaceWorktreeBranchAndCachedConfig(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	must(t, os.MkdirAll(e.root+"/cache", 0o700))
	write(t, e.root+"/cache/cs1.ssh", cmdSSHConfig)
	must(t, e.at(e.main).Down(bg, "me/feature", false))
	contains(t, strings.Join(e.api.calls, "\n"), "DELETE user/codespaces/cs1")
	equal(t, false, w.Exist())
	equal(t, false, exists(e.root+"/cache/cs1.ssh"))
	equal(t, "", sh(t, "git", "-C", e.main, "branch", "--list", "me/feature"))
}

func TestDownForceDeletesADirtyWorktree(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	write(t, w.Path+"/new.rb", "1")
	must(t, e.at(e.main).Down(bg, "me/feature", true))
	equal(t, false, w.Exist())
}

func TestDownWithoutABranchAsksThenDeletesTheWorktreeItRunsIn(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	e.prompt.interactive, e.prompt.confirm = true, true
	must(t, e.at(w.Path).Down(bg, "", false))
	equal(t, false, w.Exist())
	contains(t, e.prompt.asked[0], "Delete worktree "+w.Path)
}

func TestDownWithoutABranchStopsOnNo(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	e.prompt.interactive = true
	equal(t, ErrCancelled, e.at(w.Path).Down(bg, "", false))
	equal(t, true, w.Exist())
}

func TestDownWithoutABranchNeedsATerminal(t *testing.T) {
	e := newEnv(t)
	e.prompt.interactive = false
	w := e.created("me/feature")
	err := e.at(w.Path).Down(bg, "", false)
	mustFail(t, err, "pass the branch")
}

// --- status ---

func TestStatusListsEachWorktreeWithItsCodespaceAndState(t *testing.T) {
	e := newEnv(t)
	one := e.created("me/one")
	must(t, one.Bind(bg, "cs-one"))
	e.created("me/two")
	e.api.on("GET", "repos/me/my-app/codespaces", 200, `{"codespaces":[{"name":"cs-one","state":"Available"}]}`)
	must(t, e.at(e.main).Status(bg, StatusOptions{}))
	equal(t, "me/one\tcs-one\tAvailable\t\t"+one.Path+"\nme/two\t\t\t\t"+e.main+"-me-two\n", e.out.String())
}

func TestStatusSkipsAPrunableWorktree(t *testing.T) {
	e := newEnv(t)
	one := e.created("me/one")
	must(t, one.Bind(bg, "cs-one"))
	gone := e.created("me/gone")
	must(t, os.RemoveAll(gone.Path)) // git now lists it as prunable
	e.api.on("GET", "repos/me/my-app/codespaces", 200, `{"codespaces":[{"name":"cs-one","state":"Available"}]}`)
	must(t, e.at(e.main).Status(bg, StatusOptions{}))
	equal(t, "me/one\tcs-one\tAvailable\t\t"+one.Path+"\n", e.out.String())
}

func TestUpOnAPrunableWorktreeLetsGitSayHowToClearIt(t *testing.T) {
	e := newEnv(t)
	gone := e.created("me/gone")
	must(t, os.RemoveAll(gone.Path))
	mustFail(t, e.at(e.main).Up(bg, "me/gone", false), "missing but already registered worktree")
}

func TestDownOnAPrunableWorktreeNamesItsPath(t *testing.T) {
	e := newEnv(t)
	gone := e.created("me/gone")
	must(t, os.RemoveAll(gone.Path))
	mustFail(t, e.at(e.main).Down(bg, "me/gone", false), "no worktree at "+gone.Path)
}

func TestStatusJSONPrintsTheChosenFields(t *testing.T) {
	e := newEnv(t)
	must(t, e.created("me/one").Bind(bg, "cs-gone"))
	must(t, e.at(e.main).Status(bg, StatusOptions{Fields: []string{"branch", "base", "state"}}))
	var rows []map[string]string
	must(t, json.Unmarshal([]byte(e.out.String()), &rows))
	equal(t, 1, len(rows))
	equal(t, "me/one", rows[0]["branch"])
	equal(t, "master", rows[0]["base"])
	equal(t, "Deleted", rows[0]["state"])
	equal(t, 3, len(rows[0]))
}

func TestStatusJQAndTemplate(t *testing.T) {
	e := newEnv(t)
	must(t, e.created("me/one").Bind(bg, "cs1"))
	e.api.on("GET", "repos/me/my-app/codespaces", 200, `{"codespaces":[{"name":"cs1","state":"Available"}]}`).
		on("GET", "repos/me/my-app/codespaces", 200, `{"codespaces":[{"name":"cs1","state":"Available"}]}`)
	must(t, e.at(e.main).Status(bg, StatusOptions{Fields: []string{"branch", "state"}, JQ: `.[] | select(.state == "Available") | .branch`}))
	equal(t, "me/one\n", e.out.String())
	e.out.Reset()
	must(t, e.at(e.main).Status(bg, StatusOptions{Fields: []string{"branch"}, Template: `{{range .}}{{.branch}};{{end}}`}))
	equal(t, "me/one;", e.out.String())
}

// --- init ---

func TestInitWritesATemplateThatParsesToNoSettingsAndNeverReplacesOne(t *testing.T) {
	e := newEnv(t)
	path := e.root + "/config/gh-csd/config.yml"
	c := NewCommands(testSettings(map[string]string{"GH_CSD_CONFIG": path}, e.main, nil, nil), Deps{IO: IO{Out: &e.out, Err: &e.err}})
	must(t, c.Init())
	contains(t, e.err.String(), "wrote "+path)
	got, err := ParseConfig(Template, path, "me/my-app")
	must(t, err)
	equal(t, 0, len(got))
	mustFail(t, c.Init(), "already exists")
}

// Regression: a failed codespace check made down remove the worktree and
// its binding, orphaning a codespace that kept billing.
func TestDownStopsWhenItCannotTellIfTheCodespaceExists(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	e.api.on("GET", "user/codespaces/cs1", 502, "")
	mustFail(t, e.at(e.main).Down(bg, "me/feature", false), "checking codespace cs1")
	equal(t, true, w.Exist())
	equal(t, "cs1", bound(t, w))
}

func TestUpStopsWhenItCannotTellIfTheCodespaceExists(t *testing.T) {
	e := newEnv(t)
	must(t, e.created("me/feature").Bind(bg, "cs-old"))
	e.api.on("GET", "user/codespaces/cs-old", 502, "")
	mustFail(t, e.at(e.main).Up(bg, "me/feature", false), "checking codespace cs-old")
	equal(t, 0, len(e.fake.matching("gh", "codespace", "create")))
}

// Regression: a failed local git diff left the change set empty, and sync
// reset every dirty file in the codespace.
func TestRunStopsWhenTheLocalDiffFails(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	must(t, w.Bind(bg, "cs1"))
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").
		onSSH("git status --porcelain", Result{Out: " M stray.rb\x00"})
	gitDir := sh(t, "git", "-C", w.Path, "rev-parse", "--absolute-git-dir")
	e.fake.pass["git"] = false
	e.fake.on(Result{Out: w.Path + "\n"}, "git", "-C", w.Path, "rev-parse", "--show-toplevel").
		on(Result{Out: gitDir + "\n"}, "git", "-C", w.Path, "rev-parse", "--absolute-git-dir").
		on(Result{Out: gitDir + "\n"}, "git", "-C", w.Path, "rev-parse", "--absolute-git-dir").
		on(Result{Out: "abc\n"}, "git", "-C", w.Path, "merge-base").
		on(Result{Err: "fatal: bad object", Status: 128}, "git", "-C", w.Path, "diff")
	_, err := e.at(w.Path).Run(bg, nil)
	mustFail(t, err, "git diff -z --no-renames --name-only abc failed")
	for _, script := range e.fake.sshScripts() {
		notContains(t, script, "git cat-file")
	}
}

// Regression: an unreadable binding read as "no codespace", so down
// removed the worktree and left its codespace running.
func TestDownStopsWhenTheBindingCannotBeRead(t *testing.T) {
	e := newEnv(t)
	w := e.created("me/feature")
	gitDir := sh(t, "git", "-C", w.Path, "rev-parse", "--absolute-git-dir")
	must(t, os.Mkdir(gitDir+"/"+BindingFile, 0o755)) // a directory cannot be read as a file
	mustFail(t, e.at(e.main).Down(bg, "me/feature", false), "reading the codespace binding")
	equal(t, true, w.Exist())
}

// Regression: up -s waited for the dotfiles only when it created the
// codespace, so a retry after a plain up reported ready too early.
func TestUpStatusWaitsForTheDotfilesOnAReusedCodespace(t *testing.T) {
	e := newEnv(t)
	must(t, e.created("me/feature").Bind(bg, "cs-old"))
	e.fake.on(Result{Out: cmdSSHConfig}, "gh", "codespace", "ssh", "--config").onSSH("setup-complete", Result{Status: 1})
	must(t, e.commands(e.main, map[string]string{"ready_check": "none"}, nil).Up(bg, "me/feature", true))
	contains(t, e.err.String(), "reusing codespace cs-old")
	contains(t, e.err.String(), "dotfiles finished")
	equal(t, 30*time.Second, e.clock.now)
	equal(t, "", e.tree("me/feature").Phase(bg))
}

// Paths with characters git would quote without -z, a newline among them,
// are found as they are.
func TestWorktreesWithUnusualPathsAreFound(t *testing.T) {
	e := newEnv(t)
	odd := e.root + "/José\nwt"
	sh(t, "git", "-C", e.main, "worktree", "add", "-q", "-b", "me/odd", odd, "origin/master")
	equal(t, odd, e.tree("me/odd").Path)
	all, err := All(bg, ExecRunner{}, e.main, "master")
	must(t, err)
	equal(t, 1, len(all))
	equal(t, odd, all[0].Path)
}
