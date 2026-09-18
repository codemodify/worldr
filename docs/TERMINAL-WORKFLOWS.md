# Native terminal task workflows

The native terminal's **Tasks** deck turns a completed command into a reusable
recipe without replacing the PTY or shell. Recipes run in the terminal's current
shell context, so aliases, virtual environments, job control, and ordinary TUI
programs continue to behave as they do when the command is typed by hand.

Open **Runs** with `Ctrl+Shift+K`. With optional OSC 133 shell integration
enabled, select a command and press `T` to save it. Open **Tasks** with
`Ctrl+Shift+T`. The deck shows each recipe and the last observed state (`never`,
`staged`, `sent`, `running`, `done`, or an exit status). Status updates require OSC 133 command
boundaries; recipes remain usable without that integration.

Task dispatch always requires a focused, explicit action:

- `Enter` expands or folds the recipe so its exact command can be reviewed.
- `S` stages the command at the shell prompt **without a newline**. Edit it or
  press Enter in the shell to execute it.
- `Ctrl+Enter` sends the selected command, then one unmodified Enter key, and
  executes it. Keeping Enter outside bracketed paste preserves this behavior in
  shells that make pasted line breaks editable.
- `Ctrl+Shift+C` copies the command, and `Delete` removes the recipe.

Opening Tasks, selecting a row, starting worldr, or restoring a workspace never
sends terminal input. Workspace state contains only the recipe name and one-line
command text. It does not persist a process, a pending dispatch, an observed
status, an environment, or a replay instruction. A restored terminal is a fresh
configured shell in its saved working directory, and every restored recipe is
in the `never` state.

Each terminal retains at most 32 recipes, each command is at most 4096 bytes,
and the full deck is at most 64 KiB. Names and commands must be valid UTF-8;
commands are single printable lines. The bounds keep manifest decoding and the
native UI predictable. Tasks and other history tools stay unavailable while an
alternate-screen program owns the terminal.
