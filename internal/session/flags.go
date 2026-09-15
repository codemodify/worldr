package session

import (
	"flag"
	"fmt"
	"io"

	"github.com/codemodify/worldr/internal/version"
)

func versionString() string { return version.String() }

func newFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("worldr-session", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func bindFlags(fs *flag.FlagSet, o *Options) {
	fs.StringVar(&o.Shell, "shell", "", "path to worldr-shell (default: sibling of worldr-session, then PATH)")
	fs.StringVar(&o.User, "user", "", "autologin this name (must be the current uid; no PAM)")
	fs.BoolVar(&o.Login, "login", false, "prompt for username (must match the current uid; no PAM)")
	fs.StringVar(&o.Desktop, "desktop", DefaultDesktop, "XDG_CURRENT_DESKTOP / XDG_SESSION_DESKTOP")
	fs.StringVar(&o.RuntimeDir, "runtime-dir", "", "XDG_RUNTIME_DIR (default: env, then /run/user/UID)")
	fs.BoolVar(&o.PrintEnv, "print-env", false, "print session environment and exit")
	fs.BoolVar(&o.DryRun, "dry-run", false, "resolve login/shell/env and exit without starting the shell")
}

// Usage writes the session help text.
func Usage(w io.Writer) {
	fmt.Fprintf(w, "worldr-session %s — start a worldr Wayland session (worldr-shell).\n\n", version.String())
	fmt.Fprintln(w, "Usage: worldr-session [flags] [--] [worldr-shell flags...]")
	fs := newFlagSet()
	var o Options
	bindFlags(fs, &o)
	fs.SetOutput(w)
	fs.PrintDefaults()
	fmt.Fprintln(w, "\nNo PAM / greetd. --login / --user only accept the current uid.")
	fmt.Fprintln(w, "Display managers: install contrib/wayland-sessions/worldr.desktop.")
}
