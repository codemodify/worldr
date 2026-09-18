# Native research workbench

The research workbench turns a local bounded dataset into three linked native
views: an observation table, a 2D signal chart and pickable 3D points in the
workspace depth pass. Selecting an observation in the table, with Up/Down, or
by clicking its spatial point updates every view. Dragging the plot area or a
point orbits the spatial view; the wheel changes its depth scale. X, Y and Z
cycle through numeric columns, and **Line / Points** changes the 2D chart.

Open the included example directly:

```sh
./bin/worldr-shell --backend=nested \
  --research=examples/data/orbit-signals.csv --project=examples/data
```

Files routes `.csv`, `.tsv` and `.worldr-data.json` to the workbench only after
Enter, double-click or **Open**. Ordinary `.json` stays in the text preview, so
opening project configuration does not unexpectedly create a dashboard. The
`--research` option is repeatable for up to eight independent dashboards.

CSV and TSV use their first row as column names. JSON datasets are an array of
flat objects whose values are strings, finite numbers, booleans or null. JSON
column order is deterministic. At least one column must contain finite numeric
data; X/Y/Z choose only numeric columns. Missing values are allowed and skipped
in plots. Nested JSON values are rejected.

The loader accepts a regular file up to 16 MiB, 20,000 rows, 32 columns and
500,000 cells. Individual displayed cells are bounded to 512 bytes. Parsing
runs off the frame loop through one worker slot. The source is checked every
two seconds and a changed file refreshes without replacing the dashboard or its
placement; **Reload** forces a read even when timestamp and size are unchanged.

Workspace state records the anchored source path, exact stable slot, selected
observation, axes, chart mode, orbit and zoom. It never stores executable code
or automatically starts a data-producing process. A missing source is reported
as an independent restore failure and retains its saved placement association.
The dashboard exposes its controls and selected observation through the native
semantic stream.

This is an exploration surface, not a statistics package, spreadsheet, query
engine or simulation solver. Larger datasets, transforms and domain-specific
analysis should live in a native SDK app or produce a bounded file for this
workbench.
