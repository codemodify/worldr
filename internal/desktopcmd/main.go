// Package desktopcmd owns the shared Worldr desktop command entry point.
package desktopcmd

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
	"github.com/codemodify/worldr/internal/plasma"
	"github.com/codemodify/worldr/internal/workspace"
)

// Main runs the desktop and retains the legacy shell command behavior.
func Main() {
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
	factory := func(opt app.Options) (experience.Experience, error) {
		if opt.Experience == "navigator" {
			return workspace.NewNavigator()
		}
		if opt.Experience == "plasma" {
			return plasma.New()
		}
		if opt.Experience == "axial" {
			return workspace.New()
		}
		return workspace.NewDesktop()
	}
	if err := app.RunExperiments(ctx, os.Stdout, opt, factory); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
