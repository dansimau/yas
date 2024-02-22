package xexec

import "io"

// createMultiWriter is like io.MultiWriter except that it checks if writers
// are nil before adding them to the output.
func createMultiWriter(writers ...io.Writer) io.Writer {
	allWriters := []io.Writer{}
	for _, w := range writers {
		if w == nil {
			continue
		}

		allWriters = append(allWriters, w)
	}

	return io.MultiWriter(allWriters...)
}
