package session

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/codemodify/worldr/internal/version"
)

func Parse(args []string) (Options, error) {
	var options Options
	set := flag.NewFlagSet("worldr-session", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&options.Shell, "shell", "", "path to worldr-shell")
	set.StringVar(&options.Desktop, "desktop", DefaultDesktop, "XDG desktop name")
	set.StringVar(&options.RuntimeDir, "runtime-dir", "", "existing private XDG runtime directory")
	set.BoolVar(&options.PrintEnv, "print-env", false, "print the session environment and exit")
	set.BoolVar(&options.DryRun, "dry-run", false, "resolve environment, command and arguments without launching")
	set.DurationVar(&options.ShutdownTimeout, "shutdown-timeout", DefaultShutdownTimeout, "maximum graceful shell shutdown time")
	if err := set.Parse(args); err != nil {
		return Options{}, err
	}
	if options.ShutdownTimeout < 250*time.Millisecond || options.ShutdownTimeout > 5*time.Minute {
		return Options{}, fmt.Errorf("--shutdown-timeout must be between 250ms and 5m")
	}
	options.ShellArgs = append([]string(nil), set.Args()...)
	return options, nil
}

func Usage(output io.Writer) {
	fmt.Fprintf(output, "worldr-session %s — start a worldr display-manager session\n\n", version.String())
	fmt.Fprintln(output, "Usage: worldr-session [session flags] [-- worldr-shell flags]")
	var options Options
	set := flag.NewFlagSet("worldr-session", flag.ContinueOnError)
	set.SetOutput(output)
	set.StringVar(&options.Shell, "shell", "", "path to worldr-shell")
	set.StringVar(&options.Desktop, "desktop", DefaultDesktop, "XDG desktop name")
	set.StringVar(&options.RuntimeDir, "runtime-dir", "", "existing private XDG runtime directory")
	set.BoolVar(&options.PrintEnv, "print-env", false, "print the session environment and exit")
	set.BoolVar(&options.DryRun, "dry-run", false, "resolve environment, command and arguments without launching")
	set.DurationVar(&options.ShutdownTimeout, "shutdown-timeout", DefaultShutdownTimeout, "maximum graceful shell shutdown time")
	set.PrintDefaults()
	fmt.Fprintln(output, "\nAuthentication is performed by the display manager; worldr-session never switches users.")
}
