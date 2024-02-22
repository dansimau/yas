package main

import (
	"os"

	"github.com/dansimau/yas/pkg/yascli"
)

func main() {
	os.Exit(yascli.Run(os.Args[1:]...))
}
