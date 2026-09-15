// Package session is the worldr-session manager: env, optional login gate,
// and launching worldr-shell. No PAM / greetd (name must match the uid).
package session

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	DefaultDesktop = "worldr"
	shellName      = "worldr-shell"
)

// Options are worldr-session CLI flags.
type Options struct {
	Shell      string
	User       string
	Login      bool
	Desktop    string
	RuntimeDir string
	PrintEnv   bool
	DryRun     bool
	ShellArgs  []string
}

// ParseFlags reads worldr-session args. Remaining args are worldr-shell flags.
func ParseFlags(args []string) (Options, error) {
	var o Options
	fs := newFlagSet()
	bindFlags(fs, &o)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.ShellArgs = fs.Args()
	if o.Desktop == "" {
		o.Desktop = DefaultDesktop
	}
	return o, nil
}

// CurrentUsername is the process uid name (USER fallback).
func CurrentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return strings.TrimSpace(os.Getenv("USER"))
}

// CheckLogin accepts want when it equals current (case-sensitive).
func CheckLogin(want, current string) error {
	want = strings.TrimSpace(want)
	current = strings.TrimSpace(current)
	if current == "" {
		return fmt.Errorf("login: cannot determine current user")
	}
	if want == "" {
		return fmt.Errorf("login: empty username")
	}
	if want != current {
		return fmt.Errorf("login: %q is not the current user %q (no PAM; cannot switch users)", want, current)
	}
	return nil
}

// PromptUsername reads a login name from in after writing a prompt.
func PromptUsername(in io.Reader, out io.Writer) (string, error) {
	if out != nil {
		fmt.Fprint(out, "worldr login: ")
	}
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("login: no username")
	}
	return strings.TrimSpace(sc.Text()), nil
}

// ResolveUser returns the username that passed the gate.
// --user skips the prompt; --login reads in. Neither → current user.
func ResolveUser(o Options, current string, in io.Reader, out io.Writer) (string, error) {
	if current == "" {
		current = CurrentUsername()
	}
	switch {
	case o.User != "":
		if err := CheckLogin(o.User, current); err != nil {
			return "", err
		}
		return current, nil
	case o.Login:
		name, err := PromptUsername(in, out)
		if err != nil {
			return "", err
		}
		if err := CheckLogin(name, current); err != nil {
			return "", err
		}
		return current, nil
	default:
		if current == "" {
			return "", fmt.Errorf("login: cannot determine current user")
		}
		return current, nil
	}
}

// FindShell locates worldr-shell: --shell, sibling of argv0, then PATH.
func FindShell(explicit, argv0 string) (string, error) {
	if explicit != "" {
		return resolveExec(explicit)
	}
	if argv0 != "" {
		dir := filepath.Dir(argv0)
		if dir != "" && dir != "." {
			cand := filepath.Join(dir, shellName)
			if p, err := resolveExec(cand); err == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(shellName); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("worldr-shell not found (pass --shell or install next to worldr-session)")
}

func resolveExec(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	if st.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", path)
	}
	return filepath.Abs(path)
}

// Env is the session environment overlay (XDG_*).
type Env struct {
	RuntimeDir     string
	SessionType    string
	Desktop        string
	SessionClass   string
	SessionUser    string
	DesktopSession string
}

// PrepareEnv builds XDG session vars. Does not mutate os.Environ.
func PrepareEnv(o Options, username string) (Env, error) {
	dir := strings.TrimSpace(o.RuntimeDir)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	}
	if dir == "" {
		uid := os.Getuid()
		dir = filepath.Join("/run/user", fmt.Sprintf("%d", uid))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			dir = filepath.Join(os.TempDir(), fmt.Sprintf("worldr-%d", uid))
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return Env{}, fmt.Errorf("XDG_RUNTIME_DIR: %w", err)
			}
		}
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return Env{}, fmt.Errorf("XDG_RUNTIME_DIR %s: %w", dir, err)
	}
	desk := o.Desktop
	if desk == "" {
		desk = DefaultDesktop
	}
	return Env{
		RuntimeDir:     dir,
		SessionType:    "wayland",
		Desktop:        desk,
		SessionClass:   "user",
		SessionUser:    username,
		DesktopSession: desk,
	}, nil
}

// Overlay returns KEY=value lines to merge onto the process environment.
func (e Env) Overlay() []string {
	out := []string{
		"XDG_RUNTIME_DIR=" + e.RuntimeDir,
		"XDG_SESSION_TYPE=" + e.SessionType,
		"XDG_CURRENT_DESKTOP=" + e.Desktop,
		"XDG_SESSION_DESKTOP=" + e.Desktop,
		"XDG_SESSION_CLASS=" + e.SessionClass,
		"DESKTOP_SESSION=" + e.DesktopSession,
	}
	if e.SessionUser != "" {
		out = append(out, "USER="+e.SessionUser, "LOGNAME="+e.SessionUser)
	}
	return out
}

// MergeEnviron applies Overlay onto base (os.Environ-style), replacing keys.
func MergeEnviron(base []string, overlay []string) []string {
	drop := map[string]bool{}
	for _, kv := range overlay {
		if i := strings.IndexByte(kv, '='); i > 0 {
			drop[kv[:i]] = true
		}
	}
	out := make([]string, 0, len(base)+len(overlay))
	for _, kv := range base {
		i := strings.IndexByte(kv, '=')
		if i <= 0 || drop[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, overlay...)
}

// Run prepares the session and starts worldr-shell (or prints env / dry-run).
func Run(stdout, stderr io.Writer, stdin io.Reader, argv0 string, o Options) error {
	user, err := ResolveUser(o, "", stdin, stderr)
	if err != nil {
		return err
	}
	env, err := PrepareEnv(o, user)
	if err != nil {
		return err
	}
	if o.PrintEnv || o.DryRun {
		fmt.Fprintf(stdout, "worldr-session %s user=%s desktop=%s\n", versionString(), user, env.Desktop)
		for _, kv := range env.Overlay() {
			fmt.Fprintln(stdout, kv)
		}
		if o.DryRun {
			shell, err := FindShell(o.Shell, argv0)
			if err != nil {
				fmt.Fprintf(stdout, "shell: (missing) %v\n", err)
			} else {
				fmt.Fprintf(stdout, "shell: %s\n", shell)
			}
			if len(o.ShellArgs) > 0 {
				fmt.Fprintf(stdout, "args: %s\n", strings.Join(o.ShellArgs, " "))
			}
		}
		return nil
	}
	shell, err := FindShell(o.Shell, argv0)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "worldr-session %s: user=%s desktop=%s shell=%s\n", versionString(), user, env.Desktop, shell)
	return startShell(shell, o.ShellArgs, MergeEnviron(os.Environ(), env.Overlay()), stdin, stdout, stderr)
}

func startShell(path string, args []string, environ []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(path, args...)
	cmd.Env = environ
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", path, err)
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(ch)
	go func() {
		sig := <-ch
		if cmd.Process != nil {
			_ = cmd.Process.Signal(sig)
		}
	}()
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}
