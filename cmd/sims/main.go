package main

import (
	"fmt"
	"os"

	"github.com/alessandro-festa/sims/pkg/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
