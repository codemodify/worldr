# Native notes

Worldr notes are native document surfaces for short engineering logs, research
observations and working prose. Files opens only names ending in
`.worldr-note.md`; ordinary Markdown, text and source files remain read-only
previews unless another application handles them.

The editor supports multiline UTF-8 text, pointer selection, Shift selection,
Home/End, arrow and page navigation, Ctrl+A, Ctrl+Z/Ctrl+Y, and Ctrl+C/X/V.
Text input uses the current XKB keymap and the nested host's text-input-v3 IME.
Cursor movement and deletion follow grapheme boundaries, so combining marks and
emoji ZWJ sequences remain intact. Its semantic tree exposes the document and
Save, Undo and Redo actions to Worldr's accessibility stream.

Ctrl+S or **Save** writes a file-backed note. The editor creates a private
temporary file in the same directory, preserves the existing permission bits,
syncs the contents, atomically renames it, and syncs the directory. It records a
SHA-256 identity when opening or saving and refuses to overwrite the path if its
contents changed externally. Reopen the file to accept that external version.

**New native note** in the application launcher creates an untitled buffer. It
does not choose a hidden path and currently has no Save As dialog. An untitled
buffer is included in a workspace checkpoint only when Worldr runs with
`--state=PATH`; without a saved workspace document it disappears when the
session exits. The first close request on any modified note shows an unsaved
warning, and a second close discards it.

Each note is limited to 48 KiB, and a workspace can contain eight note windows.
Those limits keep all note buffers, including worst-case JSON escaping, inside
Worldr's bounded 1 MiB workspace recovery envelope. A checkpoint retains exact
text, caret, selection, horizontal and vertical scroll, source association,
disk identity and dirty state. Restoring a file-backed note whose disk copy has
changed preserves the checkpoint text and reports the conflict before saving.
