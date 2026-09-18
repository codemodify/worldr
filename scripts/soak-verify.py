#!/usr/bin/env python3
"""Bounded validators and fixture seeding for scripts/soak.sh."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import pathlib
import stat
import struct
import tempfile
from typing import List, Optional, Tuple
import zlib


class ValidationError(RuntimeError):
    pass


def fail(message: str) -> None:
    raise ValidationError(message)


def load_object(path: pathlib.Path, label: str) -> dict:
    try:
        with path.open("r", encoding="utf-8") as source:
            value = json.load(source)
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        fail(f"invalid {label} {path}: {error}")
    if not isinstance(value, dict):
        fail(f"{label} must be a JSON object: {path}")
    return value


def atomic_json(path: pathlib.Path, value: dict) -> None:
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as destination:
            json.dump(value, destination, indent=2)
            destination.write("\n")
            destination.flush()
            os.fsync(destination.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def exact_integer(value: object, label: str, minimum: int = 0) -> int:
    if type(value) is not int or value < minimum:
        fail(f"{label} must be an integer >= {minimum}")
    return value


def finite_number(value: object, label: str, positive: bool = False) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        fail(f"{label} must be a finite number")
    result = float(value)
    if not math.isfinite(result) or result < 0 or positive and result <= 0:
        fail(f"{label} must be {'positive' if positive else 'nonnegative'} and finite")
    return result


def validate_duration_report(value: object, label: str, samples: Optional[int] = None) -> int:
    if not isinstance(value, dict):
        fail(f"metrics {label} must be an object")
    actual_samples = exact_integer(value.get("samples"), f"metrics {label}.samples")
    if samples is not None and actual_samples != samples:
        fail(f"metrics {label}.samples does not match rendered frames")
    values = {}
    for field in ("mean_ms", "p50_ms", "p95_ms", "p99_ms", "max_ms"):
        values[field] = finite_number(value.get(field), f"metrics {label}.{field}")
    if (
        not values["p50_ms"] <= values["p95_ms"] <= values["p99_ms"] <= values["max_ms"]
        or values["mean_ms"] > values["max_ms"]
    ):
        fail(f"metrics {label} duration summary is inconsistent")
    return actual_samples


def validate_metrics(path: pathlib.Path, backend: str, width: int, height: int) -> None:
    metrics = load_object(path, "metrics")
    if metrics.get("version") != 1:
        fail(f"unsupported metrics version in {path}")
    if metrics.get("backend") != backend:
        fail(f"metrics backend changed in {path}: {metrics.get('backend')!r}")
    if metrics.get("Width") != width or metrics.get("Height") != height:
        fail(f"metrics extent changed in {path}: {metrics.get('Width')}x{metrics.get('Height')}")
    frames = exact_integer(metrics.get("frames"), "metrics frames", 1)
    finite_number(metrics.get("seconds"), "metrics seconds", positive=True)
    if metrics.get("error"):
        fail(f"run reported an error: {metrics['error']}")
    exact_integer(metrics.get("graphics_recoveries"), "metrics graphics_recoveries")
    exact_integer(metrics.get("resizes"), "metrics resizes")
    exact_integer(metrics.get("input_events"), "metrics input_events")
    validate_duration_report(metrics.get("Work"), "Work", frames)
    validate_duration_report(metrics.get("Render"), "Render", frames)
    interval_samples = max(0, frames - 1)
    validate_duration_report(metrics.get("FrameInterval"), "FrameInterval", interval_samples)
    validate_duration_report(metrics.get("DispatchToRenderReturn"), "DispatchToRenderReturn")
    exact_integer(metrics.get("sampled_go_heap_peak_bytes"), "metrics sampled_go_heap_peak_bytes")
    exact_integer(metrics.get("process_rss_peak_bytes"), "metrics process_rss_peak_bytes")
    gpu = metrics.get("gpu")
    if not isinstance(gpu, dict):
        fail("metrics gpu must be an object")
    allocated = exact_integer(gpu.get("AllocatedBytes"), "metrics gpu.AllocatedBytes")
    peak = exact_integer(gpu.get("PeakBytes"), "metrics gpu.PeakBytes")
    exact_integer(gpu.get("BudgetBytes"), "metrics gpu.BudgetBytes", 1)
    exact_integer(gpu.get("Images"), "metrics gpu.Images")
    exact_integer(gpu.get("Buffers"), "metrics gpu.Buffers")
    if peak < allocated:
        fail("metrics GPU peak is below the final allocation")
    if not isinstance(metrics.get("measurement"), str) or not metrics["measurement"]:
        fail("metrics measurement description is missing")


def validate_png(path: pathlib.Path, width: int, height: int) -> None:
    try:
        data = path.read_bytes()
    except OSError as error:
        fail(f"cannot read snapshot {path}: {error}")
    if not data.startswith(b"\x89PNG\r\n\x1a\n"):
        fail(f"snapshot is not a PNG: {path}")
    offset = 8
    chunks: List[bytes] = []
    dimensions: Optional[Tuple[int, int]] = None
    pixel_format: Optional[Tuple[int, int, int, int, int]] = None
    saw_iend = False
    while offset < len(data):
        if len(data) - offset < 12:
            fail(f"snapshot has a truncated PNG chunk: {path}")
        size = struct.unpack(">I", data[offset : offset + 4])[0]
        kind = data[offset + 4 : offset + 8]
        end = offset + 12 + size
        if end > len(data):
            fail(f"snapshot has a truncated {kind!r} chunk: {path}")
        payload = data[offset + 8 : offset + 8 + size]
        checksum = struct.unpack(">I", data[offset + 8 + size : end])[0]
        if zlib.crc32(kind + payload) & 0xFFFFFFFF != checksum:
            fail(f"snapshot has a corrupt {kind!r} chunk: {path}")
        if offset == 8:
            if kind != b"IHDR" or size != 13:
                fail(f"snapshot does not begin with a valid IHDR: {path}")
            dimensions = struct.unpack(">II", payload[:8])
            pixel_format = struct.unpack(">BBBBB", payload[8:])
        elif kind == b"IHDR":
            fail(f"snapshot has more than one IHDR: {path}")
        if kind == b"IDAT":
            chunks.append(payload)
        if kind == b"IEND":
            if size != 0 or end != len(data):
                fail(f"snapshot has an invalid IEND or trailing bytes: {path}")
            saw_iend = True
            break
        offset = end
    if dimensions != (width, height):
        fail(f"snapshot extent changed in {path}: {dimensions}")
    if pixel_format != (8, 2, 0, 0, 0):
        fail(f"snapshot is not non-interlaced 8-bit RGB in {path}: {pixel_format}")
    if not chunks or not saw_iend:
        fail(f"snapshot is missing image data or IEND: {path}")
    try:
        pixels = zlib.decompress(b"".join(chunks))
    except zlib.error as error:
        fail(f"snapshot image data is corrupt in {path}: {error}")
    stride = 1 + width * 3
    if len(pixels) != stride * height:
        fail(f"snapshot scanline size is invalid in {path}: {len(pixels)} bytes")
    if any(pixels[offset] > 4 for offset in range(0, len(pixels), stride)):
        fail(f"snapshot uses an invalid row filter in {path}")


def validate_run(metrics: pathlib.Path, snapshot: pathlib.Path, backend: str, width: int, height: int) -> None:
    validate_metrics(metrics, backend, width, height)
    validate_png(snapshot, width, height)


def seed_session(
    state_path: pathlib.Path,
    expected_path: pathlib.Path,
    note_path: pathlib.Path,
    untitled_path: pathlib.Path,
    task_command: str,
) -> None:
    envelope = load_object(state_path, "workspace state")
    if envelope.get("version") != 2 or envelope.get("experience_id") != "worldr.workspace":
        fail("bootstrap did not produce a version-2 Worldr workspace")
    session = envelope.get("session")
    if not isinstance(session, dict) or session.get("version") != 1:
        fail("bootstrap did not produce a version-1 native session")
    terminals = session.get("terminals")
    if not isinstance(terminals, list) or len(terminals) != 1 or terminals[0].get("key") != "native:terminal":
        fail("bootstrap did not produce exactly one native terminal")
    axial = session.get("axial")
    if not isinstance(axial, dict) or axial.get("key") != "native:axial-07":
        fail("bootstrap did not produce the hosted AXIAL application")
    terminals[0]["tasks"] = [{"name": "Replay must stay inert", "command": task_command}]

    note_bytes = note_path.read_bytes()
    note_text = note_bytes.decode("utf-8")
    untitled_text = untitled_path.read_text(encoding="utf-8")
    session["notes"] = [
        {
            "key": "native:note",
            "source": str(note_path.resolve(strict=True)),
            "text": note_text,
            "disk_hash": hashlib.sha256(note_bytes).hexdigest(),
            "caret": len(note_bytes),
            "anchor": len(note_bytes),
            "top_line": 0,
            "left": 0,
        },
        {
            "key": "native:note-2",
            "text": untitled_text,
            "caret": len(untitled_text.encode("utf-8")),
            "anchor": len(untitled_text.encode("utf-8")),
            "top_line": 0,
            "left": 0,
            "dirty": True,
        },
    ]
    # A paused, non-default state must round-trip byte-for-value. A playing
    # timeline legitimately advances and therefore cannot prove exact restore.
    session["axial"] = {
        "key": "native:axial-07",
        "time": 7.25,
        "yaw": 0.31,
        "pitch": -0.17,
        "zoom": 0.12,
        "selected": 2,
        "exploded": True,
    }
    atomic_json(state_path, envelope)
    atomic_json(expected_path, {"version": 1, "state": envelope.get("state"), "session": session})


def describe_difference(expected: object, actual: object, path: str = "checkpoint") -> str:
    if type(expected) is not type(actual):
        return f"{path} type changed from {type(expected).__name__} to {type(actual).__name__}"
    if isinstance(expected, dict):
        if expected.keys() != actual.keys():
            missing = sorted(expected.keys() - actual.keys())
            added = sorted(actual.keys() - expected.keys())
            return f"{path} keys changed (missing={missing}, added={added})"
        for key in expected:
            if expected[key] != actual[key]:
                return describe_difference(expected[key], actual[key], f"{path}.{key}")
    elif isinstance(expected, list):
        if len(expected) != len(actual):
            return f"{path} length changed from {len(expected)} to {len(actual)}"
        for index, value in enumerate(expected):
            if value != actual[index]:
                return describe_difference(value, actual[index], f"{path}[{index}]")
    return f"{path} changed from {expected!r} to {actual!r}"


def validate_session(
    state_path: pathlib.Path,
    expected_path: pathlib.Path,
    root: pathlib.Path,
    dataset_path: pathlib.Path,
    note_path: pathlib.Path,
    untitled_path: pathlib.Path,
    task_command: str,
    establish_layout: bool = False,
) -> None:
    envelope = load_object(state_path, "workspace state")
    expected = load_object(expected_path, "expected checkpoint")
    if envelope.get("version") != 2 or envelope.get("experience_id") != "worldr.workspace":
        fail("workspace envelope identity changed")
    if stat.S_IMODE(state_path.stat().st_mode) != 0o600:
        fail("workspace state is not private mode 0600")
    if stat.S_IMODE(expected_path.stat().st_mode) != 0o600:
        fail("expected checkpoint is not private mode 0600")
    session = envelope.get("session")
    if not isinstance(session, dict) or session.get("version") != 1:
        fail("native session disappeared")

    project = session.get("project") or {}
    if pathlib.Path(project.get("root", "")) != root.resolve():
        fail("Files root changed across restart")
    terminals = session.get("terminals") or []
    expected_tasks = [{"name": "Replay must stay inert", "command": task_command}]
    if (
        len(terminals) != 1
        or terminals[0].get("key") != "native:terminal"
        or terminals[0].get("tasks") != expected_tasks
    ):
        fail("terminal identity or inert task recipe changed across restart")
    if pathlib.Path(terminals[0].get("directory", "")) != root.resolve():
        fail("terminal working directory changed across restart")

    models = session.get("models") or []
    research = session.get("research") or []
    if len(models) != 1 or models[0].get("key") != "native:model":
        fail("model inspector identity changed across restart")
    if len(research) != 1 or research[0].get("key") != "native:research-workbench":
        fail("research workbench identity changed across restart")
    if pathlib.Path(research[0].get("source", "")) != dataset_path.resolve():
        fail("research dataset association changed across restart")

    axial = session.get("axial") or {}
    expected_axial = (expected.get("session") or {}).get("axial")
    if axial != expected_axial:
        fail(describe_difference(expected_axial, axial, "session.axial"))

    notes = {entry.get("key"): entry for entry in session.get("notes") or []}
    if set(notes) != {"native:note", "native:note-2"}:
        fail("native notes were lost or duplicated across restart")
    note_bytes = note_path.read_bytes()
    file_note = notes["native:note"]
    if pathlib.Path(file_note.get("source", "")) != note_path.resolve():
        fail("file-backed note association changed across restart")
    if (
        file_note.get("text") != note_bytes.decode("utf-8")
        or file_note.get("disk_hash") != hashlib.sha256(note_bytes).hexdigest()
        or file_note.get("dirty", False)
    ):
        fail("file-backed native note contents or disk identity changed")
    untitled_text = untitled_path.read_text(encoding="utf-8")
    recovery_note = notes["native:note-2"]
    if (
        recovery_note.get("source")
        or recovery_note.get("text") != untitled_text
        or recovery_note.get("dirty") is not True
    ):
        fail("untitled dirty note did not survive exact session recovery")

    layouts = ((envelope.get("state") or {}).get("applications") or {}).get("layouts") or []
    keys = [entry.get("key") for entry in layouts if entry.get("key")]
    if len(keys) != len(set(keys)):
        fail("workspace contains duplicate stable layout keys")
    required = {
        "native:project-browser",
        "native:terminal",
        "native:model",
        "native:research-workbench",
        "native:axial-07",
        "native:note",
        "native:note-2",
        "sdk:dev.worldr.example.instrument/orbital",
    }
    missing = sorted(required.difference(keys))
    if missing:
        fail(f"workspace lost native/SDK layout keys: {missing}")

    if session != expected.get("session"):
        fail(describe_difference(expected.get("session"), session, "checkpoint.session"))
    actual_state = envelope.get("state")
    if establish_layout:
        expected_state = expected.get("state")
        if not isinstance(expected_state, dict) or not isinstance(actual_state, dict):
            fail("checkpoint state must remain an object")
        expected_without_layouts = json.loads(json.dumps(expected_state))
        actual_without_layouts = json.loads(json.dumps(actual_state))
        expected_applications = expected_without_layouts.get("applications") or {}
        actual_applications = actual_without_layouts.get("applications") or {}
        expected_layouts = expected_applications.pop("layouts", [])
        actual_layouts = actual_applications.pop("layouts", [])
        if expected_without_layouts != actual_without_layouts:
            fail(describe_difference(expected_without_layouts, actual_without_layouts, "checkpoint.state"))
        actual_by_key = {entry.get("key"): entry for entry in actual_layouts if entry.get("key")}
        expected_keys = {entry.get("key") for entry in expected_layouts if entry.get("key")}
        new_keys = set(actual_by_key).difference(expected_keys)
        if new_keys != {"native:note", "native:note-2"}:
            fail(f"first restore added unexpected layout keys: {sorted(new_keys)}")
        for entry in expected_layouts:
            key = entry.get("key")
            if key and actual_by_key.get(key) != entry:
                fail(
                    describe_difference(
                        entry, actual_by_key.get(key), f"checkpoint.state.applications.layouts[{key!r}]"
                    )
                )
        expected["state"] = actual_state
        atomic_json(expected_path, expected)
    elif actual_state != expected.get("state"):
        fail(describe_difference(expected.get("state"), actual_state, "checkpoint.state"))


def png_bytes(width: int, height: int) -> bytes:
    def chunk(kind: bytes, payload: bytes) -> bytes:
        return (
            struct.pack(">I", len(payload))
            + kind
            + payload
            + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF)
        )

    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    rows = b"".join(b"\x00" + b"\x10\x20\x30" * width for _ in range(height))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")


def expect_failure(callback, phrase: str) -> None:
    try:
        callback()
    except ValidationError as error:
        if phrase not in str(error):
            fail(f"self-test expected {phrase!r}, got {error!r}")
    else:
        fail(f"self-test did not reject {phrase}")


def self_test() -> None:
    with tempfile.TemporaryDirectory(prefix="worldr-soak-verify-") as temporary:
        root = pathlib.Path(temporary)
        metrics_path, snapshot_path = root / "metrics.json", root / "snapshot.png"
        frames = 2
        duration = {"samples": frames, "mean_ms": 1.0, "p50_ms": 1.0, "p95_ms": 1.1, "p99_ms": 1.2, "max_ms": 1.3}
        interval = dict(duration, samples=frames - 1)
        metrics = {
            "version": 1,
            "backend": "headless",
            "seconds": 0.1,
            "Width": 2,
            "Height": 1,
            "frames": frames,
            "graphics_recoveries": 0,
            "resizes": 0,
            "input_events": 0,
            "Work": duration,
            "Render": duration,
            "FrameInterval": interval,
            "DispatchToRenderReturn": {"samples": 0, "mean_ms": 0, "p50_ms": 0, "p95_ms": 0, "p99_ms": 0, "max_ms": 0},
            "sampled_go_heap_peak_bytes": 1,
            "process_rss_peak_bytes": 1,
            "gpu": {"AllocatedBytes": 1, "PeakBytes": 1, "BudgetBytes": 2, "Images": 1, "Buffers": 1},
            "measurement": "self-test",
        }
        metrics_path.write_text(json.dumps(metrics), encoding="utf-8")
        snapshot_path.write_bytes(png_bytes(2, 1))
        validate_run(metrics_path, snapshot_path, "headless", 2, 1)
        damaged = bytearray(snapshot_path.read_bytes())
        damaged[-8] ^= 1
        snapshot_path.write_bytes(damaged)
        expect_failure(lambda: validate_png(snapshot_path, 2, 1), "corrupt")
        snapshot_path.write_bytes(png_bytes(2, 1))
        metrics["frames"] = 0
        metrics_path.write_text(json.dumps(metrics), encoding="utf-8")
        expect_failure(lambda: validate_metrics(metrics_path, "headless", 2, 1), "frames")

        note, untitled, dataset = root / "note.md", root / "untitled.txt", root / "data.csv"
        note.write_text("file note\n", encoding="utf-8")
        untitled.write_text("dirty note\n", encoding="utf-8")
        dataset.write_text("x,y\n0,1\n", encoding="utf-8")
        task = "touch /tmp/worldr-soak-self-test"
        note_bytes = note.read_bytes()
        session = {
            "version": 1,
            "project": {"root": str(root)},
            "terminals": [
                {
                    "key": "native:terminal",
                    "directory": str(root),
                    "tasks": [{"name": "Replay must stay inert", "command": task}],
                }
            ],
            "models": [{"key": "native:model"}],
            "research": [{"key": "native:research-workbench", "source": str(dataset)}],
            "notes": [
                {
                    "key": "native:note",
                    "source": str(note),
                    "text": note.read_text(),
                    "disk_hash": hashlib.sha256(note_bytes).hexdigest(),
                },
                {"key": "native:note-2", "text": untitled.read_text(), "dirty": True},
            ],
            "axial": {
                "key": "native:axial-07",
                "time": 7.25,
                "yaw": 0.31,
                "pitch": -0.17,
                "zoom": 0.12,
                "selected": 2,
                "exploded": True,
            },
        }
        keys = [
            "native:project-browser",
            "native:terminal",
            "native:model",
            "native:research-workbench",
            "native:axial-07",
            "native:note",
            "native:note-2",
            "sdk:dev.worldr.example.instrument/orbital",
        ]
        state = {"applications": {"layouts": [{"key": key, "x": index} for index, key in enumerate(keys)]}}
        envelope = {"version": 2, "experience_id": "worldr.workspace", "state": state, "session": session}
        expected = {"version": 1, "state": state, "session": session}
        state_path, expected_path = root / "workspace.json", root / "expected.json"
        atomic_json(state_path, envelope)
        initial_expected = json.loads(json.dumps(expected))
        initial_expected["state"]["applications"]["layouts"] = [
            entry for entry in initial_expected["state"]["applications"]["layouts"]
            if entry.get("key") not in {"native:note", "native:note-2"}
        ]
        atomic_json(expected_path, initial_expected)
        validate_session(state_path, expected_path, root, dataset, note, untitled, task, establish_layout=True)
        if load_object(expected_path, "expected checkpoint") != expected:
            fail("self-test did not establish the first restored note layouts")
        validate_session(state_path, expected_path, root, dataset, note, untitled, task)
        envelope["state"]["applications"]["layouts"][0]["x"] = 99
        atomic_json(state_path, envelope)
        expect_failure(
            lambda: validate_session(state_path, expected_path, root, dataset, note, untitled, task),
            "layouts",
        )
        envelope["state"]["applications"]["layouts"][0]["x"] = 0
        envelope["session"]["axial"]["selected"] = 1
        atomic_json(state_path, envelope)
        expect_failure(
            lambda: validate_session(state_path, expected_path, root, dataset, note, untitled, task),
            "selected",
        )


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser()
    commands = result.add_subparsers(dest="command", required=True)
    run = commands.add_parser("run")
    run.add_argument("metrics", type=pathlib.Path)
    run.add_argument("snapshot", type=pathlib.Path)
    run.add_argument("backend")
    run.add_argument("width", type=int)
    run.add_argument("height", type=int)
    seed = commands.add_parser("seed")
    seed.add_argument("state", type=pathlib.Path)
    seed.add_argument("expected", type=pathlib.Path)
    seed.add_argument("note", type=pathlib.Path)
    seed.add_argument("untitled", type=pathlib.Path)
    seed.add_argument("task_command")
    session = commands.add_parser("session")
    session.add_argument("state", type=pathlib.Path)
    session.add_argument("expected", type=pathlib.Path)
    session.add_argument("root", type=pathlib.Path)
    session.add_argument("dataset", type=pathlib.Path)
    session.add_argument("note", type=pathlib.Path)
    session.add_argument("untitled", type=pathlib.Path)
    session.add_argument("task_command")
    session.add_argument("--establish-layout", action="store_true")
    commands.add_parser("self-test")
    return result


def main() -> None:
    arguments = parser().parse_args()
    if arguments.command == "run":
        validate_run(arguments.metrics, arguments.snapshot, arguments.backend, arguments.width, arguments.height)
    elif arguments.command == "seed":
        seed_session(arguments.state, arguments.expected, arguments.note, arguments.untitled, arguments.task_command)
    elif arguments.command == "session":
        validate_session(
            arguments.state,
            arguments.expected,
            arguments.root,
            arguments.dataset,
            arguments.note,
            arguments.untitled,
            arguments.task_command,
            arguments.establish_layout,
        )
    else:
        self_test()


if __name__ == "__main__":
    try:
        main()
    except ValidationError as error:
        raise SystemExit(str(error)) from error
