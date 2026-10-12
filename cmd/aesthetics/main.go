// Command aesthetics evaluates software artifacts against configurable
// qualitative guidelines.
package main

import (
	"os"

	"github.com/sadasant/aesthetics/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
