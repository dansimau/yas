package main

import (
	"os"

	"github.com/dansimau/yas/pkg/workyardcli"
)

func main() {
	os.Exit(workyardcli.Run(os.Args[1:]...))
}
