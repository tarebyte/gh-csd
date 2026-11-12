package csd

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const approvalMessage = "authorize or deny additional permissions"

// Clock lets tests advance time instantly.
type Clock interface {
	Now() time.Duration
	Pause(time.Duration)
}

// RealClock is the wall clock.
type RealClock struct{ start time.Time }

func NewRealClock() *RealClock             { return &RealClock{time.Now()} }
func (c *RealClock) Now() time.Duration    { return time.Since(c.start) }
func (c *RealClock) Pause(d time.Duration) { time.Sleep(d) }

// Wait runs check until it succeeds or the timeout passes. check runs at
// least once, and once more at the deadline.
func Wait(clock Clock, timeout, interval time.Duration, check func() (bool, error)) (bool, error) {
	deadline := clock.Now() + timeout
	for {
		ok, err := check()
		if ok || err != nil {
			return ok, err
		}
		if clock.Now() >= deadline {
			return false, nil
		}
		clock.Pause(interval)
	}
}

// Codespaces calls gh codespace and the codespaces API.
type Codespaces struct {
	runner Runner
	clock  Clock
	// Open opens a URL in the browser, for the permissions approval.
	Open func(url string) error
	log  io.Writer
}

func (c *Codespaces) Exist(name string) bool {
	return c.runner.Capture([]string{"gh", "codespace", "view", "-c", name}, "").OK()
}

// Create makes a codespace on the base branch. The devcontainer can ask for
// extra repository permissions; until the user approves them, gh (without
// a terminal) prints an approval URL and fails: open it, wait for the
// approval, and create again.
func (c *Codespaces) Create(displayName string, s *Settings) (string, error) {
	if len(displayName) > 48 {
		displayName = displayName[:48]
	}
	args := []string{"gh", "codespace", "create", "-R", s.Repo(), "-b", s.Branch(), "-m", s.Machine(),
		"--devcontainer-path", s.Devcontainer(), "-d", displayName}
	if s.DefaultPermissions() {
		args = append(args, "--default-permissions")
	}
	res := c.runner.Capture(args, "")
	if name := nameFrom(res); name == "" && strings.Contains(res.Err, approvalMessage) {
		if err := c.approve(regexp.MustCompile(`https://\S+`).FindString(res.Err), s); err != nil {
			return "", err
		}
		res = c.runner.Capture(args, "")
	}
	if name := nameFrom(res); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("gh codespace create failed: %s", strings.TrimSpace(res.Err))
}

func (c *Codespaces) PermissionsAccepted(s *Settings) bool {
	res := c.runner.Capture([]string{"gh", "api", "-X", "GET", "repos/" + s.Repo() + "/codespaces/permissions_check",
		"-f", "ref=" + s.Branch(), "-f", "devcontainer_path=" + s.Devcontainer(), "--jq", ".accepted"}, "")
	return res.OK() && strings.TrimSpace(res.Out) == "true"
}

// Machines lists the repo's machine types, as -m takes them.
func (c *Codespaces) Machines(s *Settings) ([]string, error) {
	res := c.runner.Capture([]string{"gh", "api", "repos/" + s.Repo() + "/codespaces/machines", "--jq", ".machines[].name"}, "")
	if !res.OK() {
		return nil, fmt.Errorf("listing machine types failed: %s", strings.TrimSpace(res.Err))
	}
	return lines(res.Out), nil
}

// States maps the repo's codespaces to their state (Available, Shutdown...).
func (c *Codespaces) States(s *Settings) (map[string]string, error) {
	res := c.runner.Capture([]string{"gh", "codespace", "list", "-R", s.Repo(), "--json", "name,state",
		"--jq", `.[] | .name + " " + .state`}, "")
	if !res.OK() {
		return nil, fmt.Errorf("gh codespace list failed: %s", strings.TrimSpace(res.Err))
	}
	states := map[string]string{}
	for _, line := range lines(res.Out) {
		name, state, _ := strings.Cut(line, " ")
		states[name] = state
	}
	return states, nil
}

func (c *Codespaces) Delete(name string) error {
	res := c.runner.Capture([]string{"gh", "codespace", "delete", "-c", name, "--force"}, "")
	if !res.OK() {
		return fmt.Errorf("gh codespace delete failed: %s", strings.TrimSpace(res.Err))
	}
	return nil
}

func (c *Codespaces) approve(url string, s *Settings) error {
	if url == "" {
		return fmt.Errorf("gh asked for a permissions approval without a URL")
	}
	fmt.Fprintf(c.log, "gh csd: approve the codespace's extra permissions in your browser: %s\n", url)
	_ = c.Open(url)
	ok, _ := Wait(c.clock, 10*time.Minute, 5*time.Second, func() (bool, error) { return c.PermissionsAccepted(s), nil })
	if !ok {
		return fmt.Errorf("no approval after 10 minutes; run up again, or pass --default-permissions")
	}
	return nil
}

// nameFrom reads the new codespace's name: gh prints progress first and the
// name last.
func nameFrom(res Result) string {
	if !res.OK() {
		return ""
	}
	all := lines(res.Out)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
