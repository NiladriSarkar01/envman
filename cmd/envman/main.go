package main

import (
	"fmt"
	"os"

	"github.com/envman/internal/cli"
)

func main() {
	// The executable owns process-level concerns; command handlers only return
	// errors, while this boundary reports them and signals failure to the shell.
	if err := cli.Execute(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
