package main

import (
	"os"

	"github.com/openzro/openzro/client/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
