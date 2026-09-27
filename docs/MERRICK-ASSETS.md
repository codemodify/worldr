# Merrick desktop artwork

The reference desktop is built from native SDK surfaces, host window geometry,
and interactive controls. Only the wallpaper and fictional personnel portrait
are raster artwork. Both were made with the built-in image-generation tool,
then copied into the repository and embedded in the native binaries.

| Asset | Project path | Use |
| --- | --- | --- |
| Sculpted silver wallpaper | `internal/workspace/assets/merrick-silver.png` | The `sculpted-silver` desktop backdrop |
| Fictional personnel portrait | `examples/merrick-desktop/assets/personnel-portrait.png` | The profile's Visual Archive panel |

The wallpaper edit used the user-provided reference, also available locally at
`/home/user/Temp5/window-borders/merrickdesk_l.jpg`. The source is not needed to
build or run the project. The portrait is a newly generated fictional adult.

## Wallpaper prompt

> Use case: precise-object-edit. Asset type: full-screen desktop wallpaper for the existing native app project. Edit the supplied reference by removing ALL interface elements: every window, calendar, menu, side rail, tab, document, portrait, icon and text, including the full left and right vertical rails. Reconstruct only the underlying silver-white organic sculpture wallpaper seamlessly under those regions. Preserve the original large smooth biomorphic folded loops, rounded voids, flowing tubular ribbons, grayscale satin ceramic / polished matte aluminum material, soft studio highlights, subtle cloudy reflections and deep grey folds. Same wide landscape framing and composition as the reference; full bleed edge to edge, 2048x896 or equivalent wide resolution. The result must be ONLY the clean pale silver abstract 3D sculpture wallpaper, with absolutely no UI, no labels, no lettering, no border, no people.

## Portrait prompt

> Use case: photorealistic-natural. Asset type: portrait photograph inside a fictional futuristic personnel record application. A fictional adult blonde woman with hair pulled tightly back, centered frontal head-and-shoulders ID portrait, calm neutral expression and eyes looking directly at camera, pale neutral skin with natural fine texture, wearing a simple high-collared white clinical uniform. Plain soft light gray studio background. Straight-on orthographic-looking portrait camera, gentle even white studio light, subtly cool color balance, sharp eyes and realistic facial detail, restrained early-2000s science-fiction film production design. Portrait aspect 3:4. No text, no logos, no decorative frame, no UI, no jewelry, no science fiction circuitry.
