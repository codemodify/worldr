# Hologram Disk Management

An interactive disk-management reference built with the public `nativeapp`, `nativeui`, and `skin` SDKs. The dark table, luminous cyan / gold / violet partition blocks, perspective grid, particles, and edge halos are rendered procedurally in Go. The client uses no generated images or external assets. Partition geometry is an interactive isometric drawing inside a native application surface; the host draws the actual window frame.

Run the full workspace from the repository root:

```sh
./scripts/run-hologram-desktop.sh
```

The launcher builds the app and shell, selects Hologram, and seeds a separate workspace from [layout.json](layout.json). Subsequent launches retain positions in `dist/hologram-desktop/state.json`.

Build, render a client PNG, or run the focused tests:

```sh
go build -o bin/hologram-desktop ./examples/hologram-desktop
go run ./examples/hologram-desktop --snapshot=/tmp/hologram-disk.png
go run ./examples/hologram-desktop --skin plasma --snapshot=/tmp/plasma-disk.png
go test ./examples/hologram-desktop
```

`--skin` accepts a built-in ID or a JSON skin package path. Without `--snapshot`, the executable serves the native application protocol on standard input/output; the shell can launch it with `--native-app=./bin/hologram-desktop`. Colors, fonts, control recipes, focus feedback, and hover states come from the shared skin package. Live skin changes retain selection, sorting, filters, sample values, and keyboard focus.

- Click a table row or partition block to select that volume in both views.
- Click a column heading to sort; click it again to reverse the order. Blocks keep their physical disk order.
- The first two toolbar buttons select the previous or next visible volume. The remaining buttons toggle usage details, open help, refresh the fixture values, reset the sample view, and toggle the perspective grid.
- File, Action, View, and Help open working menus. The legend switches filter matching rows and blocks together.
- Tab moves keyboard focus; Enter or Space activates the focused control. Up and Down select adjacent visible volumes when a row or block has focus. Escape closes a menu or help.

All capacities and refresh values are fixtures. The app does not enumerate, format, modify, or persist disks. The three sample volumes are a 200 MB EFI partition, a 1861.97 GB NTFS system partition, and an 854 MB recovery partition.

The manifest ID is `dev.worldr.hologram-desktop`, the stable surface key is `volumes`, and its host key is `sdk:dev.worldr.hologram-desktop/volumes`. The initial client is 1440×1000, with an explicit 900×650 minimum. Rendering and semantic hit regions use the same resize transform. Static frames publish only after an interaction or skin/size change; closing the surface retires its texture exactly once.
