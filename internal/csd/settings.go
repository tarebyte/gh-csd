package csd

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
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
// Flags only: codespace (-c, for run) and takeover. The environment locates
// files (GH_CSD_CONFIG, XDG_CONFIG_HOME, HOME) and can name the repo
// (GH_REPO), as for gh.
type Settings struct {
	Env    map[string]string
	Cwd    string
	Flags  map[string]string
	Config map[string]string
	// CacheDir holds cached ssh configs; it must be private to the user.
	CacheDir      string
	repo          string
	defaultBranch string
}

// EnvKeys are the environment variables Settings reads.
var EnvKeys = []string{"GH_CSD_CONFIG", "GH_REPO", "XDG_CONFIG_HOME", "HOME"}

var defaults = map[string]string{
	"devcontainer":        ".devcontainer/devcontainer.json",
	"default_permissions": "false",
}

// NewSettings builds settings for the given repo and default branch ("" for
// each outside a checkout or without a GitHub remote).
func NewSettings(env map[string]string, cwd string, flags, config map[string]string, repo, defaultBranch string) *Settings {
	if flags == nil {
		flags = map[string]string{}
	}
	return &Settings{Env: env, Cwd: cwd, Flags: flags, Config: config, repo: repo, defaultBranch: defaultBranch}
}

// ForCheckout finds the repo (GH_REPO, else the origin remote of the
// checkout at cwd, with ssh host aliases resolved by translate) and reads
// the config file for it.
func ForCheckout(ctx context.Context, env map[string]string, cwd string, flags map[string]string, r Runner, translate func(*url.URL) *url.URL) (*Settings, error) {
	origin, _ := r.Capture(ctx, []string{"git", "-C", cwd, "remote", "get-url", "origin"}, "")
	head, _ := r.Capture(ctx, []string{"git", "-C", cwd, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"}, "")
	repo := RepoFrom(strings.TrimSpace(origin.Out), translate)
	if name := env["GH_REPO"]; name != "" {
		parsed, err := repository.Parse(name)
		if err != nil {
			return nil, fmt.Errorf("GH_REPO: %w", err)
		}
		repo = parsed.Owner + "/" + parsed.Name
	}
	config, err := LoadConfig(env, repo)
	if err != nil {
		return nil, err
	}
	s := NewSettings(env, cwd, flags, config, repo, strings.TrimPrefix(strings.TrimSpace(head.Out), "origin/"))
	return s, s.Validate()
}

// Validate rejects settings that would otherwise be misread.
func (s *Settings) Validate() error {
	if _, err := parseBool(s.value("default_permissions")); err != nil {
		return fmt.Errorf("default_permissions: %w", err)
	}
	return nil
}

// RequireRepo fails outside a checkout with a github.com remote.
func (s *Settings) RequireRepo() error {
	if s.repo == "" {
		return fmt.Errorf("no github.com origin remote in %s; set GH_REPO to name the repo", s.Cwd)
	}
	return nil
}

// Repo is owner/name.
func (s *Settings) Repo() string { return s.repo }

// Machine is "" when neither -m nor the config file sets it.
func (s *Settings) Machine() string { return s.value("machine") }

// SetMachine records the machine type up chose.
func (s *Settings) SetMachine(name string) { s.Flags["machine"] = name }

// ConfigPath is the config file's path.
func (s *Settings) ConfigPath() string { return ConfigPath(s.Env) }

// Devcontainer is the devcontainer.json path in the repo.
func (s *Settings) Devcontainer() string { return s.value("devcontainer") }

// DefaultPermissions skips the devcontainer's extra permissions. Validate
// has already rejected values that are not booleans.
func (s *Settings) DefaultPermissions() bool {
	on, _ := parseBool(s.value("default_permissions"))
	return on
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

// ReadyCheck is the services check, or "" for none.
func (s *Settings) ReadyCheck() string {
	if c := s.value("ready_check"); c != "none" {
		return c
	}
	return ""
}

// Codespace is -c, or "".
func (s *Settings) Codespace() string { return s.Flags["codespace"] }

// Takeover is --takeover.
func (s *Settings) Takeover() bool { return s.Flags["takeover"] == "true" }

// Home is the user's home directory.
func (s *Settings) Home() string { return s.Env["HOME"] }

func (s *Settings) value(key string) string {
	if v := s.Flags[key]; v != "" {
		return v
	}
	if v, ok := s.Config[key]; ok {
		return v
	}
	return defaults[key]
}

// parseBool reads YAML 1.1 and 1.2 booleans, which yaml.v3 leaves as
// strings when they are YAML 1.1 only (yes, no, on, off).
func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "", "false", "no", "off", "0":
		return false, nil
	case "true", "yes", "on", "1":
		return true, nil
	}
	return false, fmt.Errorf("want true or false, not %q", v)
}
