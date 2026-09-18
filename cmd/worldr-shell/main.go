// Command worldr-shell runs the native GPU scene workspace.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/codemodify/worldr/internal/app"
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/workspace"
)

func main() {
	opt, err := app.Parse(os.Args[1:], os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	factory := func() (experience.Experience, error) {
		if opt.Experience == "axial" {
			return workspace.New()
		}
		return workspace.NewDesktop()
	}
	if err := app.Run(ctx, os.Stdout, opt, factory); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
