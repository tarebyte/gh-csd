package main

import (
	"strings"
	"testing"

	"github.com/tarebyte/gh-csd/internal/csd"
)

func TestHelpAndUsageErrorsExitLikeGh(t *testing.T) {
	cases := map[string]int{
		"--help":          0,
		"up --help":       0,
		"up --nope":       1,
		"up a b":          1,
		"status --json":   1,
		"status extra":    1,
		"down -f a b":     1,
		"no-such-thing":   1,
		"init unexpected": 1,
	}
	for args, want := range cases {
		if got := execute(strings.Fields(args)); got != want {
			t.Errorf("gh csd %s: want exit %d, got %d", args, want, got)
		}
	}
}

func TestRunTakesFlagsOnlyBeforeTheCommand(t *testing.T) {
	cmd := runCmd()
	if err := cmd.ParseFlags([]string{"-c", "cs1", "--takeover", "bin/test", "-v", "--seed", "1"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cmd.Flags().Args(), " "); got != "bin/test -v --seed 1" {
		t.Errorf("args: %q", got)
	}
	if v, _ := cmd.Flags().GetString("codespace"); v != "cs1" {
		t.Errorf("codespace: %q", v)
	}
}

func TestJSONFlagChecks(t *testing.T) {
	if err := checkJSONFlags(csd.StatusOptions{Fields: []string{"branch", "state"}}); err != nil {
		t.Error(err)
	}
	if err := checkJSONFlags(csd.StatusOptions{Fields: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), `unknown JSON field: "nope"`) {
		t.Errorf("unknown field: %v", err)
	}
	if err := checkJSONFlags(csd.StatusOptions{JQ: "."}); err == nil || !strings.Contains(err.Error(), "without specifying `--json`") {
		t.Errorf("jq without json: %v", err)
	}
}
