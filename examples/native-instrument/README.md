# Native instrument SDK example

This executable imports the public `sdk/nativeapp/v1` process contract and
`sdk/nativeui/v1` control toolkit. It publishes a retained RGBA surface, partial
texture damage, a rotating spatial mesh with a refractive glass shell, semantic
controls and pointer/keyboard handling through the out-of-process protocol. Its
run control uses the public painter and follows the workspace's selected palette
and shape. The shell also demonstrates the additive v1 `Transmission`,
`Refraction` and `RefractionBlur` material fields over native instrument content.

The manifest advertises `ControlThemes`, and the app implements
`ControlThemeHandler` with `nativeui.FromControlTheme`. Change the control family
or shape in **Settings → Themes** while it is running; the button repaints live.
Apps that do not advertise this optional capability keep their own presentation.

```sh
go build -o bin/worldr-native-instrument ./examples/native-instrument
./bin/worldr-shell --backend=nested --native-app=./bin/worldr-native-instrument
```

Click **PAUSE STREAM** or press Space while the instrument is focused. Resize,
drag, throw, depth placement and workspace focus remain host responsibilities.
The example writes protocol traffic only to stdout and diagnostics only to
stderr, as required by SDK v1.
