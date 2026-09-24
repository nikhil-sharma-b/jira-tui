package ui_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikhil-sharma-b/jira-tui/internal/ui"
)

// directShell runs the command where the test is, without a terminal to hand
// over, and records each command line it was given.
func directShell(ran *[]string) ui.EditorExec {
	return func(command *exec.Cmd, done tea.ExecCallback) tea.Cmd {
		*ran = append(*ran, strings.Join(command.Args, " "))
		return func() tea.Msg { return done(command.Run()) }
	}
}

func shellDriver(t *testing.T, opts ui.Options) (*driver, *[]string) {
	t.Helper()
	ran := &[]string{}
	if opts.Client == nil {
		opts.Client = &fakeClient{issues: sampleIssues(3)}
	}
	if opts.Config == nil {
		opts.Config = testConfig(t, nil)
	}
	opts.ShellExec = directShell(ran)
	d := newPausedDriver(t, opts)
	d.flush()
	return d, ran
}

func TestShellCommandOutputIsShownInAPager(t *testing.T) {
	d, _ := shellDriver(t, ui.Options{})
	d.command(":!printf 'first\\nsecond\\n'")

	view := d.view()
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(view, want) {
			t.Errorf("pager lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "exit status") {
		t.Errorf("a successful command reported an exit status:\n%s", view)
	}

	d.keys("q")
	if strings.Contains(d.view(), "second") {
		t.Errorf("q left the pager up:\n%s", d.view())
	}
}

func TestShellPagerScrolls(t *testing.T) {
	d, _ := shellDriver(t, ui.Options{})
	d.command(":!seq 1 100")
	if strings.Contains(d.view(), "│100") {
		t.Fatalf("the whole output fitted; the test needs more lines:\n%s", d.view())
	}
	d.keys("G")
	if !strings.Contains(d.view(), "│100") {
		t.Errorf("G did not reach the end of the output:\n%s", d.view())
	}
	d.keys("g", "g")
	if strings.Contains(d.view(), "│100") {
		t.Errorf("gg did not return to the top:\n%s", d.view())
	}
	d.keys("esc")
	if strings.Contains(d.view(), "$ seq") {
		t.Errorf("Esc left the pager up:\n%s", d.view())
	}
}

func TestShellPercentExpandsToTheFocusedKey(t *testing.T) {
	d, ran := shellDriver(t, ui.Options{})
	d.keys("j")
	d.command(`:!echo key=% literal=\%`)

	if !strings.Contains(d.view(), "key=ENG-2 literal=%") {
		t.Errorf("%% did not expand to ENG-2:\n%s", d.view())
	}
	if len(*ran) != 1 {
		t.Errorf("ran %q, want one command", *ran)
	}
}

func TestShellPercentWithNoItemIsReported(t *testing.T) {
	d, ran := shellDriver(t, ui.Options{Client: &fakeClient{}})
	d.command(":!echo %")

	if len(*ran) != 0 {
		t.Errorf("ran %q with nothing for %% to name", *ran)
	}
	if got := d.statusLine(); !strings.Contains(got, "no work item for %") {
		t.Errorf("status = %q, want a report that %% has nothing to expand to", got)
	}
}

func TestShellFailureShowsOutputAndExitStatus(t *testing.T) {
	d, _ := shellDriver(t, ui.Options{})
	d.command(":!echo broken >&2; exit 3")

	view := d.view()
	if !strings.Contains(view, "broken") {
		t.Errorf("stderr is absent:\n%s", view)
	}
	if !strings.Contains(view, "exit status 3") {
		t.Errorf("exit status is absent:\n%s", view)
	}
}

func TestShellWithoutOutputSaysSo(t *testing.T) {
	d, _ := shellDriver(t, ui.Options{})
	d.command(":!true")
	if !strings.Contains(d.view(), "no output") {
		t.Errorf("a silent command showed a blank pager:\n%s", d.view())
	}
}

func TestShellWithNoCommandIsReported(t *testing.T) {
	d, ran := shellDriver(t, ui.Options{})
	d.command(":!")
	if len(*ran) != 0 {
		t.Errorf("ran %q for an empty command", *ran)
	}
	if got := d.statusLine(); !strings.Contains(got, "needs a command") {
		t.Errorf("status = %q", got)
	}
}

func TestReloadFromThePagerRereadsTheItem(t *testing.T) {
	client := &fakeClient{issues: sampleIssues(2)}
	d, _ := shellDriver(t, ui.Options{Client: client})
	d.keys("enter")
	d.flush()
	before := len(client.issueRequests())

	d.command(":!true")
	d.keys("R")
	d.flush()

	if strings.Contains(d.view(), "no output") {
		t.Errorf("R left the pager up:\n%s", d.view())
	}
	if got := len(client.issueRequests()); got != before+1 {
		t.Errorf("issue reads = %d, want %d: R after a command rereads the item", got, before+1)
	}
}

func TestAcliPassesItsArgumentsThrough(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\necho acli got \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "acli"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	d, _ := shellDriver(t, ui.Options{})
	d.command(":acli jira workitem view %")
	if !strings.Contains(d.view(), "acli got jira workitem view ENG-1") {
		t.Errorf("acli did not receive its arguments:\n%s", d.view())
	}
}

func TestAcliMissingIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	d, ran := shellDriver(t, ui.Options{})
	d.command(":acli jira workitem view %")
	if len(*ran) != 0 {
		t.Errorf("ran %q without acli installed", *ran)
	}
	if got := d.statusLine(); !strings.Contains(got, "acli is not installed") {
		t.Errorf("status = %q, want a report that acli is missing", got)
	}
}
