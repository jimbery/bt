package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/jayimbery/bt/internal/cli"
	"github.com/jayimbery/bt/internal/replay"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, replay.ErrArtifactNotFound) {
			os.Exit(2)
		}
		if errors.Is(err, cli.ErrTestFailures) || errors.Is(err, cli.ErrReplayFailurePresent) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}
