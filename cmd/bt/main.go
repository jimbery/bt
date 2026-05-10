package main

import (
	"fmt"
	"os"

	"github.com/jayimbery/bt/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := cli.ExitCodeFor(err)
		if code < 0 || code > 255 {
			code = 2
		}
		os.Exit(code)
	}
}
