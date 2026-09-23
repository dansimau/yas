package workyardcli

import (
	"fmt"
	"os"
	"strings"

	"github.com/dansimau/yas/pkg/cliutil"
)

const switchLongHelp = `Shows the workyards of the current source (as "workyard list" does) and changes
the shell's directory to the root of the one chosen. Move with the up and down
arrows, choose with Enter, cancel with Escape.

Changing directory needs the shell hook: eval "$(workyard hook zsh)" (or bash).`

type switchCmd struct{}

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

	listing, err := listYards()
	if err != nil {
		return err
	}

	if len(listing.yards) == 0 {
		return NewError("no workyards created from " + listing.source)
	}

	lines := listing.lines()
	items := make([]cliutil.SelectionItem, len(listing.yards))
	cursor := 0

	for i, meta := range listing.yards {
		items[i] = cliutil.SelectionItem{ID: meta.Target, Line: lines[i]}

		if listing.isCurrent(meta) {
			cursor = i
		}
	}

	selected, err := cliutil.InteractiveSelect(items, cursor, "Choose workyard to switch to:")
	if err != nil {
		return NewError("selection failed: " + err.Error())
	}

	// Cancelled.
	if selected == nil {
		return nil
	}

	if _, err := os.Stat(selected.ID); err != nil {
		return NewError(fmt.Sprintf("cannot switch to workyard %s: %v", selected.ID, err))
	}

	shellExec, err := shellHook.NewWriter()
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	if err := shellExec.WriteCommand("cd", selected.ID); err != nil {
		_ = shellExec.Close()

		return NewError("failed to write cd command: " + err.Error())
	}

	if err := shellExec.WriteCommand("echo", "Switched to workyard: "+selected.ID); err != nil {
		_ = shellExec.Close()

		return NewError("failed to write echo command: " + err.Error())
	}

	return shellExec.Close()
}
