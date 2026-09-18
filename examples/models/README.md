# Model inspector example

`mount.obj` is an authored, triangulated mechanical bracket with a support rib,
bolts and shaft. Coordinates use millimetres. It is an illustrative inspection
sample, not a manufacturing drawing or a structurally qualified design.

```sh
./bin/worldr-shell --backend=nested --project=examples/models --model=examples/models/mount.obj --terminal
```

Use Units to select mm. Drag the model to orbit, scroll to zoom, choose a
component, then Measure two surface points or Annotate one point. Save creates
`mount.obj.worldr-model.json` alongside the source. The OBJ itself is unchanged.
