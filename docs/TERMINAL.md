# WorldR Terminal

A separate standalone kit experiment built on `sdk/app/v1`, WorldR's native Wayland/Vulkan
application runtime. It starts your real shell on a PTY with libvterm and does
not start worldr-desktop or its experiment switcher.

For the 3D workspace with movable, throwable application panels, run
[`./scripts/run-future-panels.sh`](FUTURE-PANELS.md).

The window, glyph rendering, controls, session navigation, selection and effects
are implemented by WorldR. No existing terminal application is embedded. The
PTY connects to the chosen shell, while libvterm interprets its ANSI protocol.

```sh
./scripts/run-terminal.sh
./scripts/run-terminal.sh --directory="$HOME/projects" --theme=iris
./scripts/run-terminal.sh --command=/bin/bash -- -i
```

The reference-inspired window has a translucent dark reading plane, rounded
cyan edges, inset segmented rails and soft edge light. Cyan, Amber and Iris
palettes, opacity, edge light and motion are available in **Appearance**.
Changes settle smoothly; terminal input and shell output remain immediate.
The title area moves the real native window, its edges resize it, and the
minimize, maximize and close controls operate on that window.

**Live** is the interactive terminal. **Blocks** presents commands and their
output as independent cards, with real running/exit state, fold/expand, copy
output and Locate actions. Cards display retained terminal text and never
automatically replay commands. **Find** searches retained output, including
scrollback, with Unicode-aware highlights and previous/next navigation.

An ordinary Bash launch installs command-boundary hooks through a private
temporary rcfile after sourcing the user's `.bashrc`; no dotfile is modified.
Explicit shell arguments (for example `--command=/bin/bash -- -i`) and other
shells are left untouched. The Blocks empty state offers a copyable Bash setup
for those sessions; command cards require shell-reported OSC 133 boundaries.
Expired scrollback is marked in partially retained cards. Search leaves
alternate-screen programs alone and resumes when they exit.

Transparency is actual compositor blending with the desktop behind the window.
Background blur depends on compositor support/policy; the app does not capture
the screen or invent a background. Increase opacity for a busy wallpaper.

| Input | Action |
| --- | --- |
| Ctrl+Shift+T / + button | New shell tab, up to eight |
| Ctrl+Tab / Ctrl+Shift+Tab | Next / previous tab |
| Ctrl+Shift+W | Close current tab; close window for the last tab |
| Ctrl+Shift+C / Ctrl+Shift+V | Copy selection / paste clipboard |
| Drag text | Select visible terminal text |
| Shift+drag | Select while a TUI owns mouse reporting |
| Wheel / Shift+PageUp or PageDown | Scrollback; Shift+wheel overrides TUI mouse |
| Ctrl+plus / Ctrl+minus / Ctrl+0 | Adjust / reset text size |
| Ctrl+comma | Toggle Appearance |
| Ctrl+Shift+H | Toggle command Blocks / Live |
| Ctrl+Shift+F | Open / close Find |
| Enter / Shift+Enter in Find | Next / previous match |
| Ctrl+A, Ctrl+C, Ctrl+V in Find | Select, copy or paste query text |
| Wheel, arrows, PageUp/PageDown in Blocks | Browse command results |
| Click a block, then Enter | Expand / fold selected block |
| Escape in Blocks or Find | Return to Live |
| Tab, arrows, Enter, Escape in Appearance | Navigate controls, adjust sliders, activate, close |

Normal shell and TUI shortcuts, ANSI colors, alternate-screen programs,
bracketed paste and mouse reporting use the existing PTY engine. Exited
processes leave their last output visible; open another tab or close the window.
Appearance persists in `$XDG_CONFIG_HOME/worldr/terminal.json` (usually
`~/.config/worldr/terminal.json`). `--config=''` disables saving. Headless runs
do not use the user's settings unless `--config` is explicit.

The common text path submits glyph quads from an immutable monospace atlas.
Unsupported scripts use retained shaped glyph images. Glyphs, textures and
window resources are reused; idle frames skip GPU submission while input and
PTY polling continue. Motion can be disabled. The PTY engine's existing limits
for IME, emoji graphemes and copied soft-wrapped rows still apply; see the
[terminal engine notes](../internal/terminal/README.md).

## Native capture

```sh
./scripts/run-terminal.sh --headless --duration=1s \
  --snapshot=dist/terminal/preview.png --command=/bin/sh -- \
  -c 'printf "\033[36mWorldR Terminal\033[0m\n"; uname -sr; pwd; ls'
```

PNG exports preserve alpha. Interactive rendering uses the Vulkan swapchain
without framebuffer readback. Linux development dependencies are listed in the
repository README; the standalone window also uses `wayland-cursor`.
