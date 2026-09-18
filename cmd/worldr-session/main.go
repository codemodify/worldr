// Command worldr-session launches worldr-shell as a display-manager session.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/session"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		session.Usage(os.Stdout)
		return
	}
	options, err := session.Parse(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			session.Usage(os.Stdout)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := session.Run(os.Stdout, os.Stderr, os.Stdin, os.Args[0], options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
