# Reliability and physical qualification

Worldr separates repeatable reliability evidence from hardware observations.
The automated soak exercises the actual renderer, native PTY, Files, model
inspector, live research dataset, hosted AXIAL 3D application, native notes,
the public SDK example, snapshots, metrics, normal shutdown and exact session
reopening across several process lifetimes:

```sh
make test-soak
```

The default is three 20-minute cycles. A short smoke run uses the same path:

```sh
DURATION=10s CYCLES=2 ./scripts/soak.sh
```

Each run creates a unique directory under `dist/` containing a bootstrap log,
the freshly built SDK instrument, per-cycle logs, metrics, snapshots, note/data
fixtures, an expected checkpoint and the final workspace document. The
bootstrap produces a normal versioned checkpoint. The harness then seeds two
bounded note records, one terminal task recipe and a paused, non-default AXIAL
state before the measured restart cycles. One note remains associated with a
real `.worldr-note.md` file; the other is dirty, untitled recovery text that
exists only in the session. The first restart is allowed to allocate layout
slots for those newly seeded notes while every existing placement stays exact;
subsequent restarts compare the complete workspace state and native session to
the private expected checkpoint.

The script fails on a nonzero process exit, missing artifact, a corrupt,
truncated or wrong-size 8-bit RGB PNG, inconsistent frame metrics, a metrics or
restore error, SDK negotiation/process loss, any checkpoint value drift, lost
or duplicated native/SDK layout identities, changed note contents, or loss of
the inert task recipe. That recipe points at a unique sentinel and
the trial fails if restoration executes it; no soak action stages or runs the
recipe. Failed trials retain `status=fail` and their exit code in `run.txt`; a
detected task replay also leaves a marker inside the report. `BACKEND=nested`
runs the trial in a current Wayland desktop; `headless` is the default and is
suitable for unattended runs. The fast validator contract, including corrupt
PNG, zero-frame metrics and exact-checkpoint rejection, runs with:

```sh
make test-soak-contract
```

The prior default trial passed on 2026-09-18 on Intel Graphics (ARL). Its three
processes rendered 215,965 frames in 3,600.03 seconds with stable resource
peaks, zero graphics recoveries, all restore checks passing and the task replay
sentinel absent. That measurement predates hosted AXIAL. The AXIAL-expanded
path subsequently passed the CI-sized two-cycle, five-second gate with the
current full-checkpoint and artifact assertions. Exact measurements and limits are recorded in
[PERFORMANCE.md](PERFORMANCE.md).

Continuous integration runs formatting and static analysis, the normal and
no-CGO suites, real foot/media/X11 workflows on software Vulkan,
installed-font coverage, the race detector, the private nested input/recovery
workflow, and a two-cycle five-second version of the same restart/resume trial.
The nested target removes old evidence, writes through an absolute capture
directory and validates its metrics and PNG before passing. CI retains that
recovery evidence and the short trial's logs, expected checkpoint, metrics,
snapshots and final session document for 14 days, including artifacts from a
failed trial when they were created. `make test-integration` remains the
stricter local or hardware-runner gate for Chromium/Konsole and real GBM
DMA-BUF coverage.

Physical display, input, VT, connector and suspend behavior cannot be certified
by a headless test. From a **spare TTY**, with no graphical-session environment:

```sh
make qualify-tty
```

`scripts/try-tty.sh` refuses execution when `WAYLAND_DISPLAY`, `DISPLAY`, or a
graphical `XDG_SESSION_TYPE` is present. It records system information, the
read-only connector listing, the full log, metrics, workspace state, result and
a hardware checklist in a unique `dist/direct-*` directory. It performs one
bounded direct run and returns Worldr's exit status. Useful overrides include:

```sh
DURATION=10m CARD=/dev/dri/card1 BACKEND=vk-display \
  REPORT_DIR=/var/tmp/worldr-qualification ./scripts/try-tty.sh \
  --output=508 --output=517
```

The generated checklist covers all selected connectors, pointer and keyboard,
native window movement and throws, VT switching, suspend/resume, hotplug,
shutdown and visible corruption. Those boxes require observation on the target
machine. Keep the directory with the hardware/driver version when reporting a
pass or failure.
