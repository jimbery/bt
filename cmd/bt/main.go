package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/jayimbery/bt/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, cli.ErrTestFailures) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}
