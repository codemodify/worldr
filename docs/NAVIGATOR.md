# Native app navigation

Navigator provides a mouse and keyboard path from projects to live application
previews to a focused app. Applications keep their own controls, documents and
keyboard input while their presentation moves between views.

```sh
./scripts/run-navigator.sh
```

The launcher builds Worldr and opens a 1280×820 native window. On first launch,
it opens Files at the repository, the sample research dataset
`examples/data/orbit-signals.csv`, the model `examples/models/mount.obj`, and
the hosted AXIAL study. These are working native applications.

The first-run [workspace template](../examples/navigator-desktop/workspace.json)
groups Files under **Code**, the dataset under **Research**, and the model and
AXIAL study under **Design**. The launcher copies this template only when neither
the selected state file nor its `.autosave` recovery file exists, including for
a custom `--state=PATH`. Existing project names, app assignments and empty saved
workspaces are preserved. `--state=''` disables persistence and skips the grouped
template while still opening the starter apps.

The session is separate from other launchers:
`dist/navigator/workspace.json`. Later launches restore saved content without
adding the starter apps again; a recovery checkpoint also counts as a saved
session. `--state=PATH` chooses another session. `--fresh` starts the sample
apps again while retaining the saved layout. Normal exit saves the session.

Choose a project at **Home**, then click an app preview to focus it. **Back**
returns one level; **Home** returns to projects; **Overview** moves between the
current project's previews and its focused app. **Search** opens the command
palette, **Tools** exposes application launch actions, and **Motion** enables
or disables transitions. In a focused app, the left strip and Previous/Next
controls switch between running apps across projects. Project overviews keep
Previous/Next within that project. Tab (or Shift+Tab), arrows, and Enter navigate
project previews and the Tools menu. Escape closes navigation menus or goes back
while the overview owns input.

| Shortcut | Action |
| --- | --- |
| Ctrl+Alt+O | Toggle project overview / focused app |
| Ctrl+Alt+H | Return Home |
| Ctrl+Alt+J / K | Switch apps |
| Ctrl+Alt+Space | Search |
| Ctrl+Alt+E | Open the Experiments menu |
| Ctrl+Alt+Q | Exit Worldr |

Inside a focused app, Escape and ordinary application shortcuts belong to that
app. Use Back, Overview, or the navigation shortcuts to leave it. Preview
selection does not send the opening click through to the application.

The existing desktop remains the default. Run `--experience=navigator` to use
this presentation directly, with the usual native, Wayland, and X11 application
options. Navigator uses the shared workspace placement and session model;
switching views does not close or recreate applications.

## Switch experiments

The **Experiments** button at the bottom left is shared by all eight experiments.
Click it or press **Ctrl+Alt+E** to open the menu. Click an entry to switch, or
use the arrow keys and Enter. Escape closes the menu. The list follows the
experiments' development order, with the current Workspace Navigator last:

1. AXIAL / 07
2. Spatial Workspace
3. Skin Studio
4. Merrick Desktop
5. Advanced Studio
6. Hologram Disk Management
7. Plasma Fluid Surfaces
8. Workspace Navigator

Switching experiments saves the current layout and opens the selected
experiment's separate saved layout. Returning restores that layout and reopens
supported saved native applications. This restarts the experiment: SDK demo
control values reset, and restored terminals start fresh shell sessions. Running
processes are not retained, and external Wayland/X11 applications are not
automatically reopened. Moving between Home, Overview, and focused apps within
Navigator continues to keep the same live applications.

For a deterministic native capture with its own session:

```sh
./scripts/run-navigator.sh --backend=headless --demo --frames=360 \
  --state=dist/navigator/demo.json --snapshot=dist/navigator/demo.png
```

When Vulkan headers are outside the system include path, set
`VULKAN_HEADERS=/path/to/Vulkan-Headers` before running the launcher.
