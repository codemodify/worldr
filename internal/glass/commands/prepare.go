package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/codemodify/worldr/internal/terminal"
)

// Prepare enables command boundaries for a default interactive Bash launch.
// Explicit arguments and other shells are returned untouched. The temporary
// rcfile sources the user's ordinary .bashrc before installing the existing
// opt-in metadata hooks, and never changes a dotfile. Keep cleanup until every
// tab using the returned options has closed; it is safe to call more than once.
func Prepare(options terminal.Options) (prepared terminal.Options, cleanup func(), integrated bool, err error) {
	cleanup = func() {}
	prepared = options
	if len(options.Args) != 0 {
		return
	}
	command := options.Command
	if command == "" {
		command = os.Getenv("SHELL")
		if command == "" {
			command = "/bin/sh"
		}
	}
	if filepath.Base(command) != "bash" {
		return
	}
	resolved, resolveErr := exec.LookPath(command)
	if resolveErr != nil {
		err = fmt.Errorf("find interactive Bash: %w", resolveErr)
		return
	}
	directory, createErr := os.MkdirTemp("", "worldr-bash-")
	if createErr != nil {
		err = fmt.Errorf("create shell integration directory: %w", createErr)
		return
	}
	var once sync.Once
	cleanup = func() { once.Do(func() { _ = os.RemoveAll(directory) }) }
	path := filepath.Join(directory, "bashrc")
	content := "# WorldR session metadata; this file is temporary.\n" +
		"if [[ -r ${HOME-}/.bashrc ]]; then builtin source -- \"$HOME/.bashrc\"; fi\n" + terminal.BashIntegration
	if writeErr := os.WriteFile(path, []byte(content), 0600); writeErr != nil {
		cleanup()
		err = fmt.Errorf("write shell integration: %w", writeErr)
		return
	}
	prepared.Command = resolved
	prepared.Args = []string{"--rcfile", path, "-i"}
	integrated = true
	return
}
