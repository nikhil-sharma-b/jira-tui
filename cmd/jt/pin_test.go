package main

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func branchOK(b string) func() (string, error) {
	return func() (string, error) { return b, nil }
}

func branchErr() (string, error) { return "", errNoBranch{} }

type errNoBranch struct{}

func (errNoBranch) Error() string { return "not a repository" }

func TestResolvePin(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		env        map[string]string
		branch     func() (string, error)
		wantKey    string
		wantSource PinSource
	}{
		{
			name:       "argument wins over everything",
			args:       []string{"PROJ-1"},
			env:        map[string]string{"JT_ISSUE": "PROJ-2"},
			branch:     branchOK("feature/PROJ-3-thing"),
			wantKey:    "PROJ-1",
			wantSource: PinFromArg,
		},
		{
			name:       "env wins over branch",
			env:        map[string]string{"JT_ISSUE": "PROJ-2"},
			branch:     branchOK("feature/PROJ-3-thing"),
			wantKey:    "PROJ-2",
			wantSource: PinFromEnv,
		},
		{
			name:       "branch inferred when nothing else set",
			branch:     branchOK("feature/PROJ-3-thing"),
			wantKey:    "PROJ-3",
			wantSource: PinFromBranch,
		},
		{
			name:       "lowercase branch still matches",
			branch:     branchOK("feature/proj-42-fix"),
			wantKey:    "PROJ-42",
			wantSource: PinFromBranch,
		},
		{
			name:       "lowercase argument is normalized",
			args:       []string{"proj-7"},
			branch:     branchErr,
			wantKey:    "PROJ-7",
			wantSource: PinFromArg,
		},
		{
			name:       "branch with no key yields no pin",
			branch:     branchOK("main"),
			wantKey:    "",
			wantSource: PinNone,
		},
		{
			name:       "not a repository yields no pin",
			branch:     branchErr,
			wantKey:    "",
			wantSource: PinNone,
		},
		{
			name:       "non-key argument falls through to branch",
			args:       []string{"notakey"},
			branch:     branchOK("bug/PROJ-9-crash"),
			wantKey:    "PROJ-9",
			wantSource: PinFromBranch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, src := resolvePin(tt.args, env(tt.env), tt.branch)
			if key != tt.wantKey || src != tt.wantSource {
				t.Errorf("resolvePin() = (%q, %q), want (%q, %q)", key, src, tt.wantKey, tt.wantSource)
			}
		})
	}
}
