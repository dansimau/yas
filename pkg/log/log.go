package log

import (
	"fmt"
	"os"
)

func Info(msg ...any) {
	if os.Getenv("YAS_VERBOSE") != "" {
		fmt.Fprintln(os.Stderr, msg...)
	}
}
