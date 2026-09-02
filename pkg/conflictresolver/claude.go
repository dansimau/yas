package conflictresolver

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/dansimau/yas/pkg/xexec"
)

// ClaudeName is the name of the Claude Code resolver.
const ClaudeName = "claude"

// claudeAllowedTools restricts the Claude Code CLI to reading and editing
// files plus read-only git inspection. Anything that changes repository state
// (git add, commit, rebase, ...) is deliberately excluded: yas performs those
// steps itself after verifying the resolution.
var claudeAllowedTools = []string{
	"Read",
	"Edit",
	"Write",
	"Grep",
	"Glob",
	"Bash(git diff:*)",
	"Bash(git log:*)",
	"Bash(git show:*)",
	"Bash(git status:*)",
}

func init() {
	Register(ClaudeName, func() Resolver { return &Claude{Binary: ClaudeName} })
}

// Claude resolves conflicts by shelling out to the Claude Code CLI in
// non-interactive ("-p") mode.
type Claude struct {
	// Binary is the executable to run. Defaults to "claude".
	Binary string
}

// Name implements Resolver.
func (c *Claude) Name() string { return ClaudeName }

// CheckAvailable implements Resolver.
func (c *Claude) CheckAvailable() error {
	if _, err := exec.LookPath(c.Binary); err != nil {
		return fmt.Errorf("conflict resolver %q requires the %q command to be installed and on your PATH: %w", ClaudeName, c.Binary, err)
	}

	return nil
}

// Resolve implements Resolver.
func (c *Claude) Resolve(req Request) error {
	args := c.Args(req)

	// Stdin is explicitly detached: when it isn't a terminal, `claude -p`
	// treats stdin as additional prompt input and would otherwise block or
	// consume whatever yas itself was given.
	if err := xexec.Command(args...).
		WithWorkingDir(req.Dir).
		WithStdin(nil).
		Run(); err != nil {
		return fmt.Errorf("%s exited with an error: %w", c.Binary, err)
	}

	return nil
}

// Args returns the full command line used to invoke Claude for req.
func (c *Claude) Args(req Request) []string {
	return []string{
		c.Binary,
		"-p", Prompt(req),
		"--permission-mode", "acceptEdits",
		"--allowedTools", strings.Join(claudeAllowedTools, ","),
	}
}

// Prompt builds the instructions given to the AI tool for req.
func Prompt(req Request) string {
	var b strings.Builder

	b.WriteString("You are resolving git merge conflicts that occurred during a rebase.\n\n")
	fmt.Fprintf(&b, "The branch `%s` is being rebased onto `%s`.\n", req.Branch, req.Onto)

	if req.CommitSubject != "" {
		fmt.Fprintf(&b, "The commit currently being replayed is: %q\n", req.CommitSubject)
	}

	b.WriteString("\nBecause this is a rebase, the sides of each conflict are:\n")
	fmt.Fprintf(&b, "  - \"ours\" / HEAD (the top of the `<<<<<<<` block) is the branch being rebased onto: `%s`\n", req.Onto)
	fmt.Fprintf(&b, "  - \"theirs\" (the bottom of the `>>>>>>>` block) is the commit from `%s` being replayed\n", req.Branch)

	b.WriteString("\nThe following files contain unresolved conflicts:\n")

	for _, file := range req.Files {
		fmt.Fprintf(&b, "  - %s\n", file)
	}

	b.WriteString(`
Resolve every conflict in these files by editing them in place so that the
intent of both sides is preserved and the result is correct, consistent code.
Remove all conflict markers (<<<<<<<, =======, >>>>>>>). Do not make unrelated
changes.

Do NOT run git add, git commit, git rebase, or any other command that modifies
repository state; only edit the files. When you are done, print a short summary
of how you resolved each conflict.
`)

	return b.String()
}
