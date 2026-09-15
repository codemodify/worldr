// Command worldr-session starts a worldr Wayland session around worldr-shell.
//
// It sets XDG session environment, optionally gates on the current username
// (--login / --user; no PAM), then runs worldr-shell. Display managers should
// install contrib/wayland-sessions/worldr.desktop.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/session"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		session.Usage(os.Stdout)
		os.Exit(0)
	}
	opt, err := session.ParseFlags(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			session.Usage(os.Stdout)
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := session.Run(os.Stdout, os.Stderr, os.Stdin, os.Args[0], opt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
