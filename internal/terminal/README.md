The native terminal embeds libvterm 0.3 or newer and xkbcommon, then starts the
requested program on a real PTY using `github.com/creack/pty`. It has no Wayland,
renderer or window-system dependency. Linux builds need the `vterm` and
`xkbcommon` pkg-config packages; non-CGO builds provide an unavailable stub.

One host goroutine owns the terminal. Poll reads at most 256 KiB per call using
nonblocking syscalls, handles terminal replies and returns immutable snapshots.
Input queues, paste size, screen dimensions and scrollback memory are bounded.
Only process waiting uses another goroutine. Closing the PTY sends its normal
hangup; a bounded escalation reaps the owned shell process. On shell exit, a
WNOWAIT observation keeps its PID pinned while terminating remaining members of
that same process group, then reaps it. This does not promise to terminate
arbitrary programs that deliberately detach or create separate process groups.

Input uses the compositor's XKB keymap, modifier masks, compose table and repeat
policy. General input-method composition is not implemented. Pointer coordinates
are terminal cells; the provider handles selection and clipboard policy, while
libvterm encodes input for TUIs that enable mouse reporting. Terminal output does
not automatically read or write the system clipboard.

The frontend owns glyph coverage, fonts, selection and rendering. A cell can
contain one base code point and up to five combining characters, matching the
libvterm ABI. Wide-character continuation cells have width zero. This is not a
promise of complete emoji grapheme or shaping support.

Selection currently separates visual rows with LF. Libvterm 0.3.3 moves its
[line continuation flags before scrollback callbacks](https://github.com/neovim/libvterm/blob/v0.3.3/src/state.c#L129-L150),
and [resize/scrollback restoration does not preserve them](https://github.com/neovim/libvterm/blob/v0.3.3/src/screen.c#L647-L713).
Its public callbacks therefore cannot reliably distinguish soft wraps from hard
breaks across those operations. The backend does not infer wrapping from full
rows; reliable joined copying needs library metadata support first.

Cursor snapshots preserve application-requested visibility, shape and blink
state. The native provider honors blinking and steady block, underline and bar
styles from DECSCUSR, while unfocused cursors remain steady outlines.

Tests use real PTYs, shell line discipline, foreground job control, bracketed
paste, XKB composition, mouse reporting and Vim editing. Tests skip optional Vim
or Bash integrations when those executables are unavailable.
