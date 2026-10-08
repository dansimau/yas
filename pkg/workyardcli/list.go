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
	// names holds the name of each yard (see workyard.Metadata.Name), in
	// the order of yards.
	names []string
}

// listYards lists the yards of the source found from the current directory
// (see workyard.FindSource).
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
		return nil, WrapError(err, ExitUsage)
	}

	yards, err := workyard.List(source)
	if err != nil {
		return nil, WrapError(err, ExitFailure)
	}

	yardsDir, err := workyard.YardsDir(source)
	if err != nil {
		return nil, WrapError(err, ExitFailure)
	}

	names := make([]string, len(yards))
	for i, meta := range yards {
		names[i] = meta.Name(yardsDir)
	}

	return &yardListing{source: source, current: current, yards: yards, names: names}, nil
}

// find returns the yard whose name is query, else the only yard whose name
// contains query. It is an error when no yard matches or several do.
func (l *yardListing) find(query string) (*workyard.Metadata, error) {
	var exact, partial []int

	for i, meta := range l.yards {
		switch {
		case l.names[i] == query || meta.Target == query:
			exact = append(exact, i)
		case strings.Contains(l.names[i], query):
			partial = append(partial, i)
		}
	}

	matches := exact
	if len(matches) == 0 {
		matches = partial
	}

	switch len(matches) {
	case 0:
		return nil, NewUsageError(fmt.Sprintf("no workyard of %s matches %q", l.source, query))
	case 1:
		return &l.yards[matches[0]], nil
	}

	names := make([]string, len(matches))
	for i, index := range matches {
		names[i] = l.names[index]
	}

	return nil, NewUsageError(fmt.Sprintf("%q matches several workyards: %s", query, strings.Join(names, ", ")))
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
