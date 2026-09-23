package workyard

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// List returns the metadata of every workyard created from source, sorted by
// target path. A source without any workyards yields an empty list.
func List(source string) ([]Metadata, error) {
	source, err := realPath(source)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(filepath.Join(source, workyardDir, yardsDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var yards []Metadata

	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}

		meta, err := readMetadata(source, id)
		if err != nil {
			return nil, err
		}

		yards = append(yards, meta)
	}

	slices.SortFunc(yards, func(a, b Metadata) int {
		return strings.Compare(a.Target, b.Target)
	})

	return yards, nil
}

// Exists reports whether the workyard described by meta is still in place,
// i.e. its target directory holds the .workyard pointer.
func (m Metadata) Exists() bool {
	return isPointer(m.Target)
}
