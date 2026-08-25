package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// issueKeyPattern matches a Jira work item key: an uppercase project key, a
// hyphen, and a number.
var issueKeyPattern = regexp.MustCompile(`\b([A-Z][A-Z0-9]+-[0-9]+)\b`)

// PinSource records where a pin came from, for diagnostics.
type PinSource string

const (
	PinFromArg    PinSource = "argument"
	PinFromEnv    PinSource = "JT_ISSUE"
	PinFromBranch PinSource = "git branch"
	PinNone       PinSource = ""
)

// resolvePin determines which work item this session is bound to.
//
// Precedence is argument, then $JT_ISSUE, then inference from the current git
// branch name. A tmux session per story can set the environment variable once;
// a bare jt inside a story worktree still lands on the right item.
func resolvePin(args []string, getenv func(string) string, branch func() (string, error)) (string, PinSource) {
	if len(args) > 0 && args[0] != "" {
		if k := normalizeKey(args[0]); k != "" {
			return k, PinFromArg
		}
	}
	if v := getenv("JT_ISSUE"); v != "" {
		if k := normalizeKey(v); k != "" {
			return k, PinFromEnv
		}
	}
	if b, err := branch(); err == nil {
		if k := issueKeyPattern.FindString(strings.ToUpper(b)); k != "" {
			return k, PinFromBranch
		}
	}
	return "", PinNone
}

// normalizeKey accepts a bare key in any case and returns it uppercased, or
// empty if the input is not a work item key.
func normalizeKey(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if issueKeyPattern.MatchString(s) {
		return issueKeyPattern.FindString(s)
	}
	return ""
}

// currentBranch reports the checked-out branch in the working directory.
// A detached HEAD or a non-repository yields an error, which resolvePin treats
// as simply having no branch to infer from.
func currentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var _ = os.Getenv
