# Native accessibility adapter contract

Worldr can stream its native control semantics to an external accessibility
adapter without making the renderer or application providers depend on a
particular desktop bus. Start the shell with an absolute path inside a private
runtime directory:

```sh
./bin/worldr-shell --backend=nested \
  --accessibility-socket="$XDG_RUNTIME_DIR/worldr-accessibility.sock"
```

`worldr-session` supplies that option automatically. The socket is created with
mode `0600`; its parent must be owned by the current user and must not be group-
or world-writable. A second server cannot replace an existing socket.

Each client receives newline-delimited JSON. A newly connected client gets the
latest snapshot, followed by snapshots whose semantic content changed. The
protocol version is `1`; `serial` increases for each published change.

```json
{
  "version": 1,
  "serial": 12,
  "applications": [{
    "id": 4,
    "key": "native:media-player",
    "app_id": "worldr.media-player",
    "title": "Media / experiment.mp4",
    "coordinate_space": "application-pixels",
    "focused": true,
    "native_semantics": true,
    "nodes": [{
      "id": "play",
      "role": "button",
      "label": "Pause",
      "x": 191,
      "y": 512,
      "width": 221,
      "height": 59,
      "selected": true
    }]
  }]
}
```

Application IDs are the same ephemeral public IDs used by the workspace input
router. Node IDs are scoped to one application. Bounds use that application's
content pixels because a spatial window can be transformed in depth; an adapter
that registers with a conventional desktop accessibility service must project
those bounds into its required coordinate space.

The accepted roles are `button`, `textbox`, `menuitem`, `label`, `slider`,
`image`, `document`, and `status`.
Snapshots are limited to 32 applications, 1,024 nodes per application, and
16 KiB for each text field. At most eight clients can connect. A slow client
keeps one latest update instead of back-pressuring the frame loop.

Files exposes visible entries, tool buttons, search and operation dialogs. Media
players expose transport, position and volume. Native terminal tools, notes,
research dashboards, model inspectors and SDK v1 applications expose their
semantic trees. Compatibility applications remain one application record
without invented nodes; their own accessibility protocols are not proxied.

The version-1 socket is a read-only semantic export. It is not an AT-SPI service,
does not expose application text documents, and does not accept activation or
editing commands. Those operations require an adapter with explicit action and
screen-coordinate contracts rather than synthesizing untrusted pointer input.
