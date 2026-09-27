// Package nativeui provides portable, themed controls for Worldr native-app
// framebuffer surfaces.
//
// The package is independent of Worldr's renderer. A native app paints into an
// image.RGBA, publishes that image through sdk/nativeapp/v1, and passes the same
// nativeapp events to Controller. Controls use stable IDs so the controller can
// preserve focus and pointer capture while an app rebuilds its layout.
//
// Themes contain semantic colors and metrics instead of image assets. This lets
// applications use one control vocabulary with Instrument, Aperture, Glass, and
// Telemetry presentations, and independently choose Chamfered, Bracketed, Slab,
// or Notched geometry. Native-app v1 applications may advertise control-theme
// support and apply the workspace preference with FromControlTheme. Applications
// that do not opt in continue to own their presentation.
package nativeui
