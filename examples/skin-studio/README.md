# Skin Studio

A working out-of-process application built only on the public Worldr SDKs.
It demonstrates one interface across Merrick, Advanced, Hologram, Plasma and
custom JSON skins, with live host preference updates and preserved app state.

```sh
go build -o bin/skin-studio ./examples/skin-studio
./bin/worldr-shell --backend=nested --skin=hologram --native-app=./bin/skin-studio
./bin/skin-studio --skin=plasma --snapshot=dist/plasma-controls.png
./bin/skin-studio --export-skins=dist/skin-packages
```

Run/Pause advances the simulated analysis, Gain changes the signal, Lock holds
the gain, the two capture profiles and three channels change the waveform, and
the search field filters channels. Tab navigates, Space activates controls,
and arrow keys adjust the slider. Text entry uses native text commits.

See [the skin guide](../../docs/SKINS.md) for package authoring, host window
behavior, supported renderers, and compatibility limits.
