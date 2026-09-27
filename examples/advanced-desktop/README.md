# Advanced Studio

A working native studio interface inspired by the supplied steel-blue 2Advanced reference: an architectural hero, compact navigation, precise inset panels, and dense project and transmission columns. The Worldr branding, portfolio entries, and studio content are original demonstration material.

From the repository root:

```sh
go build -o bin/advanced-desktop ./examples/advanced-desktop
go run ./examples/advanced-desktop --snapshot dist/advanced-desktop/client.png
go run ./examples/advanced-desktop --skin plasma --snapshot /tmp/advanced-plasma.png
./bin/worldr-shell --native-app=./bin/advanced-desktop --skin=advanced
```

`--skin` accepts a built-in ID or JSON package path. `--snapshot` renders the client at 1200×960 and exits. The default executable serves the public native application protocol over standard input/output. The shell provides the window frame.

The seven navigation tabs change the feature description and hero caption. Three portfolio controls select projects. Play/Pause and Reset control a thirty-second local image reel. Updates supports text filtering, previous/next controls, pointer-wheel scrolling, and item selection. The mailing-list field validates an address and records a local subscription state; it makes no network request. The transmission checkbox switches the local channel status.

Tab traverses controls, Enter or Space activates actions, and the two text fields support native IME commits/preedit, surrounding-text replacement, Ctrl+A, selection with Shift, cursor movement, Home/End, and Unicode-aware Backspace/Delete. Enter in the email field submits the form. All controls publish semantic labels and values.

The app uses the public `nativeapp`, `nativeui`, and `skin` SDKs. Shared control recipes determine control geometry and states; semantic palette tokens determine the surrounding artwork. Optional `page-background`, `page-panel`, `page-inset`, `page-edge`, `page-text`, `page-muted`, and `heading-text` tokens refine the composition, with standard semantic fallbacks. There are no preset-ID rendering branches. The architecture image is embedded from [assets/architecture.png](assets/architecture.png); the logo, diagrams, rules, and gradients are drawn in code.

Skin changes preserve application values, cursor/selection, focus, and resource identity. Surface resizing scales drawing and pointer bounds together. The stable manifest is `dev.worldr.advanced-desktop`, the single surface key is `studio`, its initial client size is 1200×960, and its minimum is 720×600. Application data stays in memory for the running example.

```sh
go test ./examples/advanced-desktop
go vet ./examples/advanced-desktop
```

Tests cover navigation, reel timing, filtering and scroll, native text editing and validation, skin changes using custom package IDs, resize/input alignment, retained resource revisions, retirement, and the public protocol lifecycle.
