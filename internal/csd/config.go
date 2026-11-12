package csd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Template is what gh csd init writes: every setting, commented out.
const Template = `# gh csd config. Flags override these settings.
# Uncomment what you need. Top-level keys apply to every repo; a repo's
# section under repos: wins over them.

# Without machine, up asks in a terminal, or takes -m.
# machine: standardLinux32gb
# devcontainer: .devcontainer/devcontainer.json
# default_permissions: false
# dir: /workspaces/<repo name>
# branch: main
# A command that succeeds once the codespace's services accept
# connections; up retries it for up to 5 minutes.
# ready_check: none

# repos:
#   <owner/name>:
#     machine: standardLinux32gb
#     ready_check: nc -z localhost 5432
`

var repoPattern = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(?:\.git)?/?$`)

// RepoFrom returns owner/name from a github.com origin URL (SSH or HTTPS),
// or "".
func RepoFrom(url string) string {
	if m := repoPattern.FindStringSubmatch(url); m != nil {
		return m[1]
	}
	return ""
}

// ConfigPath follows the gh-dash convention for gh extensions:
// $GH_CSD_CONFIG, else $XDG_CONFIG_HOME/gh-csd/config.yml, else
// ~/.config/gh-csd/config.yml.
func ConfigPath(env map[string]string) string {
	if p := env["GH_CSD_CONFIG"]; p != "" {
		return p
	}
	base := env["XDG_CONFIG_HOME"]
	if base == "" {
		base = filepath.Join(env["HOME"], ".config")
	}
	return filepath.Join(base, "gh-csd", "config.yml")
}

// LoadConfig reads the config file; a missing file means no settings.
func LoadConfig(env map[string]string, repo string) (map[string]string, error) {
	file := ConfigPath(env)
	text, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseConfig(string(text), file, repo)
}

// ParseConfig flattens the file to strings, the repo's section over the top
// level. Nested values other than repos are ignored.
func ParseConfig(text, file, repo string) (map[string]string, error) {
	var data any
	if err := yaml.Unmarshal([]byte(text), &data); err != nil {
		return nil, fmt.Errorf("%s is not valid YAML: %w", file, err)
	}
	settings := map[string]string{}
	if data == nil {
		return settings, nil
	}
	top, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping of settings", file)
	}
	section := map[string]any{}
	if repos, ok := top["repos"].(map[string]any); ok {
		if s, ok := repos[repo].(map[string]any); ok {
			section = s
		}
	}
	for _, source := range []map[string]any{top, section} {
		for key, value := range source {
			switch value.(type) {
			case nil, map[string]any, []any:
				continue
			}
			if key != "repos" {
				settings[key] = fmt.Sprint(value)
			}
		}
	}
	return settings, nil
}
