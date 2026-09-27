# Merrick Desktop

A working seven-window reference application built entirely with the public `nativeapp`, `nativeui`, and `skin` SDKs. The pale profile panels, narrow calendar, overlapping papers, and compact palettes follow the supplied Merrick desktop reference. The records and research documents are fictional demonstration content.

Run the complete workspace from the repository root:

```sh
./scripts/run-merrick-desktop.sh
```

The [launcher](../../scripts/run-merrick-desktop.sh) builds the app and shell, selects the Merrick skin, and seeds a separate workspace at `dist/merrick-desktop/state.json` using [layout.json](layout.json). Later launches retain that workspace's window positions. The generated wallpaper and embedded personnel portrait are described in [MERRICK-ASSETS.md](../../docs/MERRICK-ASSETS.md).

Build or render client images without opening the shell:

```sh
go build -o bin/merrick-desktop ./examples/merrick-desktop
go run ./examples/merrick-desktop --snapshot-dir dist/merrick-desktop/clients
go run ./examples/merrick-desktop --skin plasma --snapshot-dir /tmp/merrick-plasma
go test ./examples/merrick-desktop
```

`--skin` accepts a built-in ID or a JSON package path. `--portrait` optionally replaces the embedded PNG with a local PNG or JPEG. Without `--snapshot-dir`, the executable serves the native application protocol over standard input/output; launch it through `worldr-shell --native-app=./bin/merrick-desktop`.

The calendar supports day selection and previous/next weeks. The profile has Status/History/Notes tabs, two archive toggles, and six adjustable sample channels. Each document has three pages. Messages switches between incoming and outgoing samples; its arrow advances the selected message. Programs reopens closed surfaces while preserving their in-process selections and pages. Tab moves focus, Enter or Space activates a focused control, and arrow keys adjust a focused range.

Every view publishes control semantics and an explicit 96×64 minimum size. Rendering and hit targets share the same coordinate transform. Changing the workspace skin preserves typed application state, focus, surface keys, and live texture identities. Calendar and document data remain local to the running example; there is no network connection or persistent record database. Static menu lettering is part of the reference composition; the actions described above are the implemented controls.

| ID | Stable key | Initial client size |
| --- | --- | --- |
| 1 | `calendar` | 190×720 |
| 2 | `records` | 890×320 |
| 3 | `archive` | 340×460 |
| 4 | `report` | 340×460 |
| 5 | `notes` | 340×460 |
| 6 | `programs` | 460×86 |
| 7 | `messages` | 230×120 |

The manifest ID is `dev.worldr.merrick-desktop`; host keys use the prefix `sdk:dev.worldr.merrick-desktop/`. Hosts with fewer available surface slots open Programs first so closed views remain discoverable.
