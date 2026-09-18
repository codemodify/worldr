# A computing environment from a plausible future

worldr should let an engineer inspect a model, a researcher explore evidence,
and someone at home read or write comfortably. Objects and their relationships
shape the workspace. Visual ambition and practical interaction both matter.

The default direction is **mostly cinematic, led by Iron Man and Avatar**:
dramatic depth, luminous structure, expressive motion, and detailed native
objects. Existing terminals and browsers must share that space with native
content. People should be able to push a group of windows into the background,
work with other objects in front, and retrieve or focus any window easily.

Presentation is selectable. Cinematic keeps its expressive framing during work;
Adaptive preserves cinematic exploration and quiets the surroundings during
focus. Both modes share native tools, compatibility, and spatial capabilities.
The workspace and study implement this preference through guides, focus borders,
authored background glow and mesh wire/rim accents. Its initial materials add light-responsive highlights and
smooth cylindrical shading in a cool blue/silver palette. The complete workspace
visual treatment remains to be built.

The product is the complete workspace. See [the product brief](WORKSPACE.md)
for the agreed spatial behavior, visual direction, current gaps, and next
acceptance workflow.

## Reference research

| Reference | Principle | Firsthand source |
| --- | --- | --- |
| Minority Report (2002) | A consistent spatial manipulation language | [John Underkoffler's account and sequence](https://vimeo.com/126756877) |
| Iron Man / MCU | Models, instruments, assistance, and tools around the activity | [Perception: Iron Man 2](https://www.experienceperception.com/work/iron-man-2/) |
| Avatar (2009) | Scientific objects and contextual information organized with meaningful depth | [Neil Huxley interview](https://inventinginteractive.com/2010/03/11/interview-neil-huxley-avatar/) |
| Moon (2009) | Clear operational information and a coherent inhabited environment | [Gavin Rothery interview](https://splendoid.net/filminutiae/interview-gavin-rothery-vfx-designer-moon-2009) |
| Her (2013) | Warmth, familiar interaction, and unobtrusive assistance | [Geoff McFetridge interview](https://www.pushing-pixels.org/2018/04/05/screen-graphics-of-her-interview-with-geoff-mcfetridge.html) |

These principles are worldr's interpretation of the references, not a claim that
all the films use one system. Studio portfolios sometimes include unused
concepts and tests in addition to shots from the released films.

## First experience: AXIAL / 07

A procedural turbine assembly sits in a dark, restrained workspace. A person
can rotate it, select parts, separate its components, scrub time, and compare
its linked synthetic response. Selection and time belong to the experience;
the scene, typography, chart, and controls reflect that same state. Its typed
document can be saved and restored, and deliberate edits can be undone. A drag
is one action; losing input focus cancels its unfinished preview.

Use generous object space, clear hierarchy, restrained cyan and amber,
meaningful motion, and quiet surrounding controls. A focus mode removes
secondary instruments. The experience should be legible on an ordinary screen
with a keyboard and mouse.

The engine retains immutable meshes on the GPU and sends model/camera constants
for each view. Typography and instruments share ordered overlay passes with the
3D scene. Native experiences own semantic actions and documents; the host owns
platform input, display, and persistence. A nested development window presents
through Vulkan directly, without a CPU image transport defining the engine.

The study is a foundation and an interaction test. It is not a simulation solver,
a CAD tool, an AI assistant, or the complete desktop. Future native tools should
use the same scene principles with their own real data and operations.

The default experience is now the general spatial workspace, with a read-only
native project browser, native PTY terminals and compatible application surfaces.
Visible grips support dragging and inertial throws, with groups, one-step Undo
and a Reduced Motion preference. AXIAL remains explicitly selectable as the
engineering experience; it is not yet a hosted native 3D application.

Real native PTY terminals and separate foot clients share space and depth
with native content. Placement, focus/return, grouping, overview, input,
clipboard and saved layout have integration tests. Chromium and Konsole also
have isolated real-client tests for SHM content, typing, popup menus and dialogs
through worldr's private Wayland server. Chromium's tested path uses software
client rendering; popup pixels stay within their root application's image.

The current acceptance work is physical direct-session qualification, longer
daily-use trials and broader everyday application coverage. The public native
SDK, editable shaped text and nested IME, a private semantic accessibility
stream, installable login-session packaging, native notes/research tools and a
bounded cinematic SDR finish are implemented. Process sandboxing, an OS AT-SPI
adapter, HDR/ICC color management and broader desktop services remain ahead.
Film references guide visual and interaction design; they do not establish that
those production capabilities already exist.
