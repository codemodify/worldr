# Native instrument SDK example

This executable imports only `sdk/nativeapp/v1`. It publishes a retained RGBA
surface, partial texture damage, a rotating spatial mesh with a refractive glass
shell, semantic controls and pointer/keyboard handling through the out-of-process
protocol. The shell demonstrates the additive v1 `Transmission`, `Refraction`
and `RefractionBlur` material fields over native instrument content.

```sh
go build -o bin/worldr-native-instrument ./examples/native-instrument
./bin/worldr-shell --backend=nested --native-app=./bin/worldr-native-instrument
```

Click **PAUSE STREAM** or press Space while the instrument is focused. Resize,
drag, throw, depth placement and workspace focus remain host responsibilities.
The example writes protocol traffic only to stdout and diagnostics only to
stderr, as required by SDK v1.
