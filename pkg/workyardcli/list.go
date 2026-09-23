package workyardcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

type listCmd struct {
	JSON bool `description:"Print the workyard metadata as JSON" long:"json"`
}

func (c *listCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	yard := current.yard

	if c.JSON {
		b, err := json.MarshalIndent(yard.Meta, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))

		return nil
	}

	fmt.Printf("workyard %s (source %s, branch %s)\n", yard.Root, yard.Meta.Source, yard.Meta.Branch)

	if !yard.Meta.Complete {
		fmt.Println("warning: this workyard was not completely created")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)

	for _, status := range yard.Status(context.Background()) {
		switch {
		case status.Missing:
			_, _ = fmt.Fprintf(w, "%s\t(missing)\t\t\n", status.Repo.Path)
		case status.Err != nil:
			_, _ = fmt.Fprintf(w, "%s\t(error: %v)\t\t\n", status.Repo.Path, status.Err)
		default:
			dirty := ""
			if status.Dirty {
				dirty = "*"
			}

			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", status.Repo.Path, status.Branch, status.Head, dirty)
		}
	}

	return w.Flush()
}
