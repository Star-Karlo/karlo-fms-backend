package ci

import (
	"os"
	"strings"
	"testing"
)

// The check script must be handed to the container through a shell variable
// filled by a quoted heredoc, never written inline between single quotes.
//
// Inline, one apostrophe in the script closes the quote. On 2026-10-30 a
// comment reading "this module's go 1.25.0" ended the quoted argument early:
// the container ran build, vet and test, echoed the lint banner and exited 0,
// while the linter install, the lint and the tidy check fell out of the
// quotes and ran on the CodeBuild host. The host carries an older Go, so the
// build failed quoting a go.mod in a dependency nobody had touched. Lint and
// the tidy check were not running at all, and a green build would have said
// nothing about them.
//
// The failure cost most of a morning and four wrong diagnoses, none of which
// looked at the comment. A heredoc ends only at its own terminator, so prose
// in the script cannot reach the shell; this test keeps it that way.
func TestBuildspecsDoNotInlineShellScriptsInSingleQuotes(t *testing.T) {
	for _, name := range []string{"../../buildspec.ci.yml", "../../buildspec.yml"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "sh -c '") {
				t.Errorf("%s:%d passes a script to sh -c between single quotes:\n\t%s\n"+
					"Build it with a quoted heredoc and pass the variable instead:\n"+
					"\tchecks=$(cat <<'CHECKS'\n\t...\n\tCHECKS\n\t)\n"+
					"\tdocker run ... sh -c \"$checks\"",
					name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
