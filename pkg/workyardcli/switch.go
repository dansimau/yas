package workyardcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/workyard"
)

const switchLongHelp = `Changes the shell's directory to another workyard of the current source. With
a name, switches to the workyard of that name (as for "workyard create":
<yards-dir>/<name>, or the path of one created with --dest); a part of a name
is enough when it matches only one. Without, shows the workyards (as
"workyard list" does) to choose from: move with the up and down arrows,
choose with Enter, cancel with Escape.

When the current directory is inside a workyard and the same relative
directory exists in the chosen one, that is where the shell ends up;
otherwise its root.

Changing directory needs the shell hook: eval "$(workyard hook zsh)" (or bash).`

type switchCmd struct {
	Args struct {
		Yard string `description:"Name (or an unambiguous part of one) of the workyard to switch to (default: choose interactively)" positional-arg-name:"name"`
	} `positional-args:"yes"`
}

func (c *switchCmd) SkipYardCheck() bool {
	return true
}

func (c *switchCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	// Fail before the user has made a choice that cannot be acted on.
	if err := shellHook.ErrIfNotInstalled(); err != nil {
		return WrapError(err, ExitUsage)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	listing, err := listYards()
	if err != nil {
		return err
	}

	if len(listing.yards) == 0 {
		return NewError("no workyards created from " + listing.source)
	}

	var target *workyard.Metadata

	if c.Args.Yard != "" {
		target, err = listing.find(c.Args.Yard)
	} else {
		target, err = listing.choose()
	}

	if err != nil || target == nil {
		return err
	}

	if _, err := os.Stat(target.Target); err != nil {
		return NewError(fmt.Sprintf("cannot switch to workyard %s: %v", target.Target, err))
	}

	dir, err := listing.sameDirIn(cwd, target.Target)
	if err != nil {
		return err
	}

	shellExec, err := shellHook.NewWriter()
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	if err := shellExec.WriteCommand("cd", dir); err != nil {
		_ = shellExec.Close()

		return NewError("failed to write cd command: " + err.Error())
	}

	if err := shellExec.WriteCommand("echo", "Switched to workyard: "+target.Target); err != nil {
		_ = shellExec.Close()

		return NewError("failed to write echo command: " + err.Error())
	}

	return shellExec.Close()
}

// choose shows the yards for the user to pick one, starting on the current
// yard. It returns nil when the user cancels.
func (l *yardListing) choose() (*workyard.Metadata, error) {
	lines := l.lines()
	items := make([]cliutil.SelectionItem, len(l.yards))
	cursor := 0

	for i, meta := range l.yards {
		items[i] = cliutil.SelectionItem{ID: meta.ID, Line: lines[i]}

		if l.isCurrent(meta) {
			cursor = i
		}
	}

	selected, err := cliutil.InteractiveSelect(items, cursor, "Choose workyard to switch to:")
	if err != nil {
		return nil, NewError("selection failed: " + err.Error())
	}

	if selected == nil {
		return nil, nil
	}

	for i, meta := range l.yards {
		if meta.ID == selected.ID {
			return &l.yards[i], nil
		}
	}

	return nil, NewError("selection failed: unknown workyard " + selected.ID)
}

// sameDirIn returns the directory in root that corresponds to cwd's place in
// the current yard, when cwd is inside a yard and that directory exists;
// otherwise root itself.
func (l *yardListing) sameDirIn(cwd, root string) (string, error) {
	if l.current == nil {
		return root, nil
	}

	rel, inside, err := relativeTo(l.current.Root, cwd)
	if err != nil {
		return "", err
	}

	if !inside || rel == "." {
		return root, nil
	}

	dir := filepath.Join(root, rel)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return root, nil
	}

	return dir, nil
}
