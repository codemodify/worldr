// Command worldr-shell is the compositor / present process.
//
// It owns the GPU present path (Vulkan display or DRM/KMS), optionally a
// minimal Wayland server, and a CPU compositor scene. See docs/RUN-ABOX.md.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/shell"
)

func main() {
	opt, err := shell.ParseFlags(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := shell.Run(os.Stdout, os.Stderr, opt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
