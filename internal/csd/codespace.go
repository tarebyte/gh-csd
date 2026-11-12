package csd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

const (
	approvalMessage = "authorize or deny additional permissions"
	// pageSize is the REST API's largest page.
	pageSize = 100
)

var approvalURL = regexp.MustCompile(`https://\S+`)

// API is the part of go-gh's REST client gh csd uses.
type API interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
}

// Clock lets tests advance time instantly.
type Clock interface {
	Now() time.Duration
	// Pause waits d, or returns ctx's error once ctx is done.
	Pause(ctx context.Context, d time.Duration) error
}

// RealClock is the wall clock.
type RealClock struct{ start time.Time }

// NewRealClock starts a clock at zero.
func NewRealClock() *RealClock { return &RealClock{time.Now()} }

// Now is the time since the clock started.
func (c *RealClock) Now() time.Duration { return time.Since(c.start) }

// Pause implements Clock.
func (c *RealClock) Pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Wait runs check until it succeeds or the timeout passes. check runs at
// least once, and once more at the deadline.
func Wait(ctx context.Context, clock Clock, timeout, interval time.Duration, check func() (bool, error)) (bool, error) {
	deadline := clock.Now() + timeout
	for {
		ok, err := check()
		if ok || err != nil {
			return ok, err
		}
		if clock.Now() >= deadline {
			return false, nil
		}
		if err := clock.Pause(ctx, interval); err != nil {
			return false, err
		}
	}
}

// Codespaces manages codespaces: gh codespace create, which owns the
// permissions approval, and the REST API for the rest.
type Codespaces struct {
	runner Runner
	api    API
	gh     string
	clock  Clock
	// Open opens a URL in the browser, for the permissions approval.
	Open func(url string) error
	log  io.Writer
}

// Exist reports whether the codespace exists. Only a 404 means it does
// not: any other failure is an error, so callers never mistake an outage
// for a deleted codespace.
func (c *Codespaces) Exist(ctx context.Context, name string) (bool, error) {
	err := c.api.DoWithContext(ctx, http.MethodGet, "user/codespaces/"+url.PathEscape(name), nil, nil)
	var httpErr *api.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking codespace %s: %w", name, err)
	}
	return true, nil
}

// Create makes a codespace on the base branch. The devcontainer can ask for
// extra repository permissions; until the user approves them, gh (without
// a terminal) prints an approval URL and fails: open it, wait for the
// approval, and create again.
func (c *Codespaces) Create(ctx context.Context, displayName string, s *Settings) (string, error) {
	if r := []rune(displayName); len(r) > 48 {
		displayName = string(r[:48])
	}
	args := []string{c.gh, "codespace", "create", "-R", s.Repo(), "-b", s.Branch(), "-m", s.Machine(),
		"--devcontainer-path", s.Devcontainer(), "-d", displayName}
	if s.DefaultPermissions() {
		args = append(args, "--default-permissions")
	}
	res, err := c.runner.Capture(ctx, args, "")
	if err != nil {
		return "", err
	}
	if nameFrom(res) == "" && strings.Contains(res.Err, approvalMessage) {
		if err := c.approve(ctx, approvalURL.FindString(res.Err), s); err != nil {
			return "", err
		}
		if res, err = c.runner.Capture(ctx, args, ""); err != nil {
			return "", err
		}
	}
	if name := nameFrom(res); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("gh codespace create failed: %s", strings.TrimSpace(res.Err))
}

// PermissionsAccepted reports whether the user approved the devcontainer's
// extra permissions.
func (c *Codespaces) PermissionsAccepted(ctx context.Context, s *Settings) (bool, error) {
	q := url.Values{"ref": {s.Branch()}, "devcontainer_path": {s.Devcontainer()}}
	var resp struct {
		Accepted bool `json:"accepted"`
	}
	err := c.api.DoWithContext(ctx, http.MethodGet, "repos/"+s.Repo()+"/codespaces/permissions_check?"+q.Encode(), nil, &resp)
	return resp.Accepted, err
}

// Machines lists the repo's machine types, as -m takes them.
func (c *Codespaces) Machines(ctx context.Context, s *Settings) ([]string, error) {
	q := url.Values{"ref": {s.Branch()}, "devcontainer_path": {s.Devcontainer()}}
	var resp struct {
		Machines []struct {
			Name string `json:"name"`
		} `json:"machines"`
	}
	if err := c.api.DoWithContext(ctx, http.MethodGet, "repos/"+s.Repo()+"/codespaces/machines?"+q.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("listing machine types: %w", err)
	}
	names := make([]string, 0, len(resp.Machines))
	for _, m := range resp.Machines {
		names = append(names, m.Name)
	}
	return names, nil
}

// States maps every one of the user's codespaces for the repo to its state
// (Available, Shutdown, ...), a page at a time.
func (c *Codespaces) States(ctx context.Context, s *Settings) (map[string]string, error) {
	states := map[string]string{}
	for page := 1; ; page++ {
		var resp struct {
			Codespaces []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"codespaces"`
		}
		path := fmt.Sprintf("repos/%s/codespaces?per_page=%d&page=%d", s.Repo(), pageSize, page)
		if err := c.api.DoWithContext(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, fmt.Errorf("listing codespaces: %w", err)
		}
		for _, cs := range resp.Codespaces {
			states[cs.Name] = cs.State
		}
		if len(resp.Codespaces) < pageSize {
			return states, nil
		}
	}
}

// Delete deletes the codespace.
func (c *Codespaces) Delete(ctx context.Context, name string) error {
	if err := c.api.DoWithContext(ctx, http.MethodDelete, "user/codespaces/"+url.PathEscape(name), nil, nil); err != nil {
		return fmt.Errorf("deleting codespace %s: %w", name, err)
	}
	return nil
}

func (c *Codespaces) approve(ctx context.Context, link string, s *Settings) error {
	if link == "" {
		return errors.New("gh asked for a permissions approval without a URL")
	}
	fmt.Fprintf(c.log, "gh csd: approve the codespace's extra permissions in your browser: %s\n", link)
	if err := c.Open(link); err != nil {
		fmt.Fprintf(c.log, "gh csd: could not open the browser: %v\n", err)
	}
	ok, err := Wait(ctx, c.clock, 10*time.Minute, 5*time.Second, func() (bool, error) {
		accepted, _ := c.PermissionsAccepted(ctx, s) // a failed check is retried, not fatal
		return accepted, ctx.Err()
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no approval after 10 minutes; run up again, or pass --default-permissions")
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
