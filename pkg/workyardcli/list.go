package workyardcli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dansimau/yas/pkg/workyard"
)

const listLongHelp = `Lists the workyards created from the current source directory, with their
branch, number of repositories and creation time. The current workyard is
marked with "*"; one whose directory is gone is marked "(missing)" and one
whose creation did not finish "(incomplete)".

The source is that of the workyard containing the current directory, else the
nearest ancestor of the current directory with a .workyard directory, else the
current directory itself.`

type listCmd struct{}

func (c *listCmd) SkipYardCheck() bool {
	return true
}

func (c *listCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	listing, err := listYards()
	if err != nil {
		return err
	}

	if len(listing.yards) == 0 {
		fmt.Fprintf(os.Stderr, "No workyards created from %s\n", listing.source)

		return nil
	}

	for _, line := range listing.lines() {
		fmt.Println(line)
	}

	return nil
}

// yardListing is the workyards created from a source, as seen from the
// current directory.
type yardListing struct {
	source string
	// current is the yard containing the current directory, or nil.
	current *workyard.Yard
	yards   []workyard.Metadata
}

// listYards lists the yards of the source of the workyard containing the
// current directory, or else of the current directory itself.
func listYards() (*yardListing, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	current, err := workyard.Find(cwd)
	if err != nil && !errors.Is(err, workyard.ErrNotAWorkyard) {
		return nil, WrapError(err, ExitUsage)
	}

	source, err := workyard.FindSource(cwd)
	if err != nil {
		return WrapError(err, ExitUsage)
	}

	yards, err := workyard.List(source)
	if err != nil {
		return nil, WrapError(err, ExitFailure)
	}

	return &yardListing{source: source, current: current, yards: yards}, nil
}

// isCurrent reports whether meta is the yard containing the current directory.
func (l *yardListing) isCurrent(meta workyard.Metadata) bool {
	return l.current != nil && l.current.ID == meta.ID
}

// lines returns one aligned line per yard, in the order of l.yards.
func (l *yardListing) lines() []string {
	var buf bytes.Buffer

	w := tabwriter.NewWriter(&buf, 0, 8, 2, ' ', 0)

	for _, meta := range l.yards {
		marker := " "
		if l.isCurrent(meta) {
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

	_ = w.Flush()

	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
}
