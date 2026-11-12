package csd

import (
	"fmt"
	"path"
	"strings"
)

// Settings are gh csd's settings. Each comes from the first of these that
// sets it: a flag, the config file (the repo's section over the top level),
// the built-in default.
//
//	config key            flag                    default
//	machine               -m, --machine           none: up asks in a terminal
//	devcontainer          --devcontainer-path     .devcontainer/devcontainer.json
//	default_permissions   --default-permissions   false
//	dir                                           /workspaces/<repo name>
//	branch                                        origin's default branch, else main
//	ready_check                                   none: no services wait
//
// Flags only: codespace (-c, for run) and takeover. The environment only
// locates files: GH_CSD_CONFIG, XDG_CONFIG_HOME, TMPDIR, HOME.
type Settings struct {
	Env           map[string]string
	Cwd           string
	Flags         map[string]string
	Config        map[string]string
	repo          string
	defaultBranch string
}

// EnvKeys are the environment variables Settings reads.
var EnvKeys = []string{"GH_CSD_CONFIG", "XDG_CONFIG_HOME", "TMPDIR", "HOME"}

var defaults = map[string]string{
	"devcontainer":        ".devcontainer/devcontainer.json",
	"default_permissions": "false",
}

// NewSettings builds settings for the given origin repo and default branch
// ("" for each outside a checkout or without an origin).
func NewSettings(env map[string]string, cwd string, flags, config map[string]string, repo, defaultBranch string) *Settings {
	if flags == nil {
		flags = map[string]string{}
	}
	return &Settings{Env: env, Cwd: cwd, Flags: flags, Config: config, repo: repo, defaultBranch: defaultBranch}
}

// ForCheckout reads the origin remote of the checkout at cwd, and the config
// file for its repo.
func ForCheckout(env map[string]string, cwd string, flags map[string]string, runner Runner) (*Settings, error) {
	url := strings.TrimSpace(runner.Capture([]string{"git", "-C", cwd, "remote", "get-url", "origin"}, "").Out)
	head := strings.TrimSpace(runner.Capture([]string{"git", "-C", cwd, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"}, "").Out)
	repo := RepoFrom(url)
	config, err := LoadConfig(env, repo)
	if err != nil {
		return nil, err
	}
	return NewSettings(env, cwd, flags, config, repo, strings.TrimPrefix(head, "origin/")), nil
}

// RequireRepo fails outside a checkout with a github.com origin.
func (s *Settings) RequireRepo() error {
	if s.repo == "" {
		return fmt.Errorf("no github.com origin remote in %s", s.Cwd)
	}
	return nil
}

func (s *Settings) Repo() string { return s.repo }

// Machine is "" when neither -m nor the config file sets it.
func (s *Settings) Machine() string        { return s.value("machine") }
func (s *Settings) SetMachine(name string) { s.Flags["machine"] = name }
func (s *Settings) ConfigPath() string     { return ConfigPath(s.Env) }
func (s *Settings) Devcontainer() string   { return s.value("devcontainer") }

func (s *Settings) DefaultPermissions() bool {
	switch s.value("default_permissions") {
	case "", "0", "false":
		return false
	}
	return true
}

// RemoteDir is where Codespaces clones the repo, unless set.
func (s *Settings) RemoteDir() string {
	if dir := s.value("dir"); dir != "" {
		return dir
	}
	return "/workspaces/" + path.Base(s.repo)
}

// Branch is the base: the codespace's checkout, and where new worktrees
// start.
func (s *Settings) Branch() string {
	if b := s.value("branch"); b != "" {
		return b
	}
	if s.defaultBranch != "" {
		return s.defaultBranch
	}
	return "main"
}

func (s *Settings) ReadyCheck() string {
	if c := s.value("ready_check"); c != "none" {
		return c
	}
	return ""
}

func (s *Settings) Codespace() string { return s.Flags["codespace"] }
func (s *Settings) Takeover() bool    { return s.Flags["takeover"] == "true" }
func (s *Settings) Home() string      { return s.Env["HOME"] }

func (s *Settings) TmpDir() string {
	if d := s.Env["TMPDIR"]; d != "" {
		return d
	}
	return "/tmp"
}

func (s *Settings) value(key string) string {
	if v := s.Flags[key]; v != "" {
		return v
	}
	if v, ok := s.Config[key]; ok {
		return v
	}
	return defaults[key]
}
