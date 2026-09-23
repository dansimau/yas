package workyardcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dansimau/yas/pkg/workyard"
)

const listLongHelp = `Lists the workyards created from a source directory, with their branch,
number of repositories and creation time. The current workyard is marked with
"*"; one whose directory is gone is marked "(missing)" and one whose creation
did not finish "(incomplete)".

Without --source, the source is that of the workyard containing the current
directory, or else the current directory itself.`

type listCmd struct {
	Source string `description:"Source directory whose workyards to list (default: the current workyard's source, or the current directory)" long:"source" short:"s"`
	JSON   bool   `description:"Print the workyard metadata as JSON"                                                                         long:"json"`
}

func (c *listCmd) SkipYardCheck() bool {
	return true
}

func (c *listCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// The yard we are in, if any: it provides the default source and is
	// marked in the output.
	current, err := workyard.Find(cwd)
	if err != nil && !errors.Is(err, workyard.ErrNotAWorkyard) {
		return WrapError(err, ExitUsage)
	}

	source := c.Source

	switch {
	case source != "":
	case current != nil:
		source = current.Source
	default:
		source = cwd
	}

	yards, err := workyard.List(source)
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	if c.JSON {
		b, err := json.MarshalIndent(yards, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))

		return nil
	}

	if len(yards) == 0 {
		fmt.Fprintf(os.Stderr, "No workyards created from %s\n", source)

		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)

	for _, meta := range yards {
		marker := " "
		if current != nil && current.ID == meta.ID {
			marker = "*"
		}

		notes := ""
		if !meta.Exists() {
			notes += "  (missing)"
		}

		if !meta.Complete {
			notes += "  (incomplete)"
		}

		_, _ = fmt.Fprintf(w, "%s %s\t%s\t%d repos\t%s%s\n",
			marker, meta.Target, meta.Branch, len(meta.Repos),
			meta.CreatedAt.Local().Format("2006-01-02 15:04"), notes)
	}

	return w.Flush()
}
