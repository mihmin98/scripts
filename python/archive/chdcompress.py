#!/usr/bin/env python3
"""
chdcompress.py -- compress disc images to .chd with maximum compression.

Examples:
    ./chdcompress.py cd  game.cue
    ./chdcompress.py cd  /roms/psx --delete
    ./chdcompress.py dvd '/roms/ps2/*.iso' -j 4 --outdir /roms/chd
"""

from __future__ import annotations

import argparse
import glob
import os
import queue
import re
import shutil
import signal
import subprocess
import sys
import threading
from dataclasses import dataclass, field
from pathlib import Path
from typing import TypedDict

try:
    from tqdm import tqdm
except ImportError:
    sys.exit("This script requires tqdm.  Install it with:  pip install tqdm")


# --------------------------------------------------------------------------- #
# Mode configuration
# --------------------------------------------------------------------------- #


class ModeCfg(TypedDict):
    command: str
    unit: int
    unit_name: str
    default_units: int
    codecs: list[str]
    exts: list[str]


MODE_CFG: dict[str, ModeCfg] = {
    "cd": {
        "command": "createcd",
        # A raw CD frame is 2352 bytes of sector data + 96 bytes of subcode.
        "unit": 2448,
        "unit_name": "frames",
        # 16 frames = 39168 bytes.  chdman's own default is 8 (19584); doubling it
        # gives the compressor more context and is still widely compatible.
        "default_units": 16,
        # chdman picks whichever of these compresses each hunk best, so listing
        # more codecs never hurts the ratio -- it only costs encoding time.
        "codecs": ["cdlz", "cdzl", "cdfl"],
        "exts": [".cue", ".gdi", ".toc", ".iso", ".nrg"],
    },
    "dvd": {
        "command": "createdvd",
        "unit": 2048,  # DVD sector
        "unit_name": "sectors",
        "default_units": 16,  # 32768 bytes
        "codecs": ["lzma", "zlib", "huff", "flac"],
        "exts": [".iso"],
    },
}

PROGRESS_RE = re.compile(
    r"(?P<verb>\w+),\s*(?P<pct>\d+(?:\.\d+)?)%\s*complete"
    r"(?:.*?ratio\s*=\s*(?P<ratio>\d+(?:\.\d+)?)%)?",
    re.IGNORECASE,
)


# --------------------------------------------------------------------------- #
# Helpers
# --------------------------------------------------------------------------- #


def human(nbytes: float) -> str:
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if abs(nbytes) < 1024 or unit == "TiB":
            return f"{nbytes:,.1f} {unit}" if unit != "B" else f"{nbytes:,.0f} B"
        nbytes /= 1024.0
    return f"{nbytes:.1f} TiB"


def resolve_chdman(override: str | None) -> str:
    """Find chdman: explicit override, then ./chdman, then PATH."""
    if override:
        p = Path(override).expanduser()
        if p.is_file() and os.access(p, os.X_OK):
            return str(p.resolve())
        found = shutil.which(override)
        if found:
            return found
        sys.exit(f"chdman not found or not executable: {override}")

    for name in ("chdman", "chdman.exe"):
        local = Path.cwd() / name
        if local.is_file() and os.access(local, os.X_OK):
            return str(local.resolve())

    found = shutil.which("chdman")
    if found:
        return found
    sys.exit("chdman not found in the current directory or on PATH (use --chdman).")


def expand_inputs(patterns: list[str], exts: list[str], recursive: bool) -> list[Path]:
    """Expand files, directories and glob patterns into a sorted list of images."""
    out: list[Path] = []
    seen: set[Path] = set()
    exts_l = {e.lower() for e in exts}

    def add(p: Path) -> None:
        rp = p.resolve()
        if rp not in seen:
            seen.add(rp)
            out.append(rp)

    for pat in patterns:
        expanded = [Path(p) for p in glob.glob(os.path.expanduser(pat), recursive=True)]
        # The shell usually expands globs itself; if it did not (quoted pattern) or
        # matched nothing, fall back to treating the argument as a literal path.
        if not expanded:
            lit = Path(os.path.expanduser(pat))
            if lit.exists():
                expanded = [lit]
            else:
                print(f"warning: no match for {pat!r}", file=sys.stderr)
                continue

        for path in expanded:
            if path.is_dir():
                it = path.rglob("*") if recursive else path.glob("*")
                for child in sorted(it):
                    if child.is_file() and child.suffix.lower() in exts_l:
                        add(child)
            elif path.is_file():
                # An explicitly named file is honoured even if its extension is
                # not in the scan list -- the user asked for it by name.
                add(path)

    return out


def cue_sidecars(cue: Path) -> list[Path]:
    """Files referenced by a .cue sheet (the .bin tracks)."""
    files: list[Path] = []
    try:
        text = cue.read_text(errors="replace")
    except OSError:
        return files
    for line in text.splitlines():
        m = re.match(r'\s*FILE\s+"([^"]+)"', line, re.IGNORECASE) or re.match(
            r"\s*FILE\s+(\S+)", line, re.IGNORECASE
        )
        if m:
            ref = (cue.parent / m.group(1)).resolve()
            if ref.is_file():
                files.append(ref)
    return files


def gdi_sidecars(gdi: Path) -> list[Path]:
    """Track files referenced by a .gdi."""
    files: list[Path] = []
    try:
        text = gdi.read_text(errors="replace")
    except OSError:
        return files
    for line in text.splitlines()[1:]:
        m = re.search(r'"([^"]+)"', line)
        name = m.group(1) if m else None
        if name is None:
            parts = line.split()
            if len(parts) >= 5:
                name = parts[4]
        if name:
            ref = (gdi.parent / name).resolve()
            if ref.is_file():
                files.append(ref)
    return files


def source_set(image: Path) -> list[Path]:
    """The image plus any files it references (for --delete and size accounting)."""
    ext = image.suffix.lower()
    if ext == ".cue":
        return [image] + cue_sidecars(image)
    if ext == ".gdi":
        return [image] + gdi_sidecars(image)
    return [image]


# --------------------------------------------------------------------------- #
# Running chdman
# --------------------------------------------------------------------------- #


@dataclass
class Result:
    image: Path
    output: Path | None = None
    ok: bool = False
    skipped: bool = False
    reason: str = ""
    src_bytes: int = 0
    dst_bytes: int = 0
    log: list[str] = field(default_factory=list)


class Cancelled(Exception):
    pass


_stop = threading.Event()


def run_chdman(cmd: list[str], bar: tqdm | None, label: str) -> tuple[int, list[str]]:
    """
    Run chdman, streaming its stderr.  chdman rewrites a single progress line with
    carriage returns, so we split on \\r as well as \\n and feed the percentage
    into a tqdm bar.  Returns (returncode, tail_of_output).
    """
    tail: list[str] = []
    kwargs: dict = {}
    if os.name == "nt":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        kwargs["start_new_session"] = True

    proc = subprocess.Popen(
        cmd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        bufsize=0,
        **kwargs,
    )

    def handle(line: str) -> None:
        m = PROGRESS_RE.search(line)
        if m and bar is not None:
            pct = float(m.group("pct"))
            bar.n = min(pct, 100.0)
            verb = m.group("verb").capitalize()
            ratio = m.group("ratio")
            suffix = f" ratio={ratio}%" if ratio else ""
            bar.set_description_str(f"  {verb} {label}{suffix}"[:70])
            bar.refresh()
        elif not m and line.strip():
            tail.append(line.strip())
            del tail[:-40]

    assert proc.stderr is not None
    fd = proc.stderr.fileno()
    buf = b""
    try:
        while True:
            if _stop.is_set():
                raise Cancelled
            try:
                chunk = os.read(fd, 4096)
            except OSError:
                break
            if not chunk:
                break
            buf += chunk
            parts = re.split(rb"[\r\n]", buf)
            buf = parts.pop()
            for part in parts:
                handle(part.decode("utf-8", "replace"))
        if buf:
            handle(buf.decode("utf-8", "replace"))
        return proc.wait(), tail
    except Cancelled:
        try:
            proc.terminate()
            proc.wait(timeout=10)
        except (OSError, subprocess.TimeoutExpired):
            proc.kill()
        raise
    finally:
        try:
            proc.stderr.close()
        except OSError:
            pass


def build_command(
    chdman: str, mode: str, image: Path, output: Path, args: argparse.Namespace
) -> list[str]:
    cfg = MODE_CFG[mode]
    cmd = [
        chdman,
        cfg["command"],
        "-i",
        str(image),
        "-o",
        str(output),
        "-c",
        ",".join(args.codec_list),
        "-hs",
        str(args.hunk_bytes),
    ]
    if args.cores:
        cmd += ["--numprocessors", str(args.cores)]
    if args.force:
        cmd += ["-f"]
    cmd += args.extra
    return cmd


def process_one(
    image: Path, mode: str, args: argparse.Namespace, chdman: str, slot: int, nbars: int
) -> Result:
    res = Result(image=image)
    sources = source_set(image)
    res.src_bytes = sum(p.stat().st_size for p in sources if p.exists())

    outdir = Path(args.outdir) if args.outdir else image.parent
    output = outdir / (image.stem + ".chd")
    res.output = output

    if output.exists() and not args.force:
        res.skipped = True
        res.reason = "output exists (use --force to overwrite)"
        return res

    if args.dry_run:
        res.skipped = True
        res.reason = "dry run: " + " ".join(
            build_command(chdman, mode, image, output, args)
        )
        return res

    outdir.mkdir(parents=True, exist_ok=True)

    bar = None
    if nbars:
        bar = tqdm(
            total=100,
            position=slot + 1,
            leave=False,
            bar_format="{desc} |{bar}| {n:.1f}%",
            dynamic_ncols=True,
        )
    try:
        cmd = build_command(chdman, mode, image, output, args)
        rc, tail = run_chdman(cmd, bar, image.name)
        if rc != 0:
            res.reason = f"chdman exited {rc}"
            res.log = tail
            if output.exists():
                output.unlink(missing_ok=True)
            return res

        # Verification is cheap insurance and is forced on when we are about to
        # delete the only other copy of the data.
        if args.verify or args.delete:
            if bar is not None:
                bar.reset(total=100)
            rc, tail = run_chdman([chdman, "verify", "-i", str(output)], bar, image.name)
            if rc != 0:
                res.reason = f"verification failed (chdman exited {rc})"
                res.log = tail
                output.unlink(missing_ok=True)
                return res

        res.ok = True
        res.dst_bytes = output.stat().st_size

        if args.delete:
            for p in sources:
                try:
                    p.unlink()
                except OSError as exc:
                    res.log.append(f"could not delete {p}: {exc}")
        return res
    except Cancelled:
        if output.exists():
            output.unlink(missing_ok=True)
        # Cancelling is not a failure; keep it out of the failed count and the
        # non-zero exit status that would otherwise imply something broke.
        res.skipped = True
        res.reason = "cancelled"
        return res
    finally:
        if bar is not None:
            bar.close()


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Compress disc images to .chd with maximum compression.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""\
Notes:
  * For CD mode, always point at the .cue / .gdi / .toc, never the raw .bin.
  * --hunk-size and --frames are two ways of saying the same thing; the unit is
    2448 bytes per frame for CD and 2048 bytes per sector for DVD.
  * Larger hunks compress better but cost more work per random read, and some
    emulators are conservative about non-default values.  Pass --frames 8 (CD)
    to fall back to chdman's stock hunk size.
  * --delete also removes the .bin/track files referenced by a .cue or .gdi.
""",
    )
    p.add_argument("mode", choices=("cd", "dvd"), help="use createcd or createdvd")
    p.add_argument(
        "inputs",
        nargs="+",
        metavar="INPUT",
        help="file, directory, or glob pattern (quote globs to let the script expand them)",
    )

    p.add_argument("--chdman", metavar="PATH", help="path to the chdman binary")
    p.add_argument("-o", "--outdir", metavar="DIR", help="write .chd files here (default: next to the source)")
    p.add_argument("-r", "--recursive", action="store_true", help="recurse into subdirectories")

    size = p.add_mutually_exclusive_group()
    size.add_argument("--hunk-size", type=int, metavar="BYTES", help="hunk size in bytes")
    size.add_argument(
        "--frames",
        "--sectors",
        dest="units",
        type=int,
        metavar="N",
        help="hunk size in CD frames (2448 B) or DVD sectors (2048 B)",
    )

    p.add_argument("-c", "--cores", type=int, metavar="N", help="cores per chdman process (default: all, split across --jobs)")
    p.add_argument("-j", "--jobs", type=int, default=1, metavar="N", help="files to convert in parallel (default: 1)")
    p.add_argument("--codecs", metavar="LIST", help="override the codec list, e.g. cdlz,cdzl,cdfl")
    p.add_argument("--ext", metavar="LIST", help="extensions to pick up when scanning directories")

    p.add_argument("--delete", action="store_true", help="delete source images after a successful, verified conversion")
    p.add_argument("--verify", action="store_true", help="run 'chdman verify' on each result (implied by --delete)")
    p.add_argument("-f", "--force", action="store_true", help="overwrite existing .chd files")
    p.add_argument("-n", "--dry-run", action="store_true", help="print the commands without running them")
    p.add_argument("-q", "--quiet", action="store_true", help="suppress per-file progress bars")
    p.add_argument("--extra", nargs=argparse.REMAINDER, default=[], help="everything after this is passed straight to chdman")

    args = p.parse_args(argv)
    cfg = MODE_CFG[args.mode]

    # Hunk size ------------------------------------------------------------- #
    unit = cfg["unit"]
    if args.hunk_size is not None:
        if args.hunk_size <= 0 or args.hunk_size % unit:
            p.error(f"--hunk-size must be a positive multiple of {unit} for {args.mode} images")
        args.hunk_bytes = args.hunk_size
    else:
        units = args.units if args.units is not None else cfg["default_units"]
        if units <= 0:
            p.error("--frames must be positive")
        args.hunk_bytes = units * unit

    # Cores / jobs ----------------------------------------------------------- #
    cpu = os.cpu_count() or 1
    if args.jobs < 1:
        p.error("--jobs must be >= 1")
    if args.cores is None:
        args.cores = max(1, cpu // args.jobs)
    elif args.cores < 1:
        p.error("--cores must be >= 1")

    args.codec_list = (
        [c.strip() for c in args.codecs.split(",") if c.strip()]
        if args.codecs
        else list(cfg["codecs"])
    )
    args.exts = (
        [e if e.startswith(".") else "." + e for e in args.ext.split(",")]
        if args.ext
        else list(cfg["exts"])
    )
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    chdman = resolve_chdman(args.chdman)

    images = expand_inputs(args.inputs, args.exts, args.recursive)
    if not images:
        print("Nothing to do: no matching images found.", file=sys.stderr)
        return 1

    # Guard against two sources mapping onto the same .chd (e.g. game.cue + game.iso)
    outdir = Path(args.outdir) if args.outdir else None
    claimed: dict[Path, Path] = {}
    unique: list[Path] = []
    for img in images:
        dest = ((outdir or img.parent) / (img.stem + ".chd")).resolve()
        if dest in claimed:
            print(f"warning: skipping {img.name}, would collide with {claimed[dest].name}", file=sys.stderr)
            continue
        claimed[dest] = img
        unique.append(img)
    images = unique

    print(f"chdman:  {chdman}")
    print(f"mode:    {args.mode} ({MODE_CFG[args.mode]['command']})")
    print(f"codecs:  {','.join(args.codec_list)}")
    print(
        f"hunk:    {args.hunk_bytes} bytes "
        f"({args.hunk_bytes // MODE_CFG[args.mode]['unit']} {MODE_CFG[args.mode]['unit_name']})"
    )
    print(f"jobs:    {args.jobs} x {args.cores} core(s)")
    print(f"files:   {len(images)}\n")

    def on_sigint(signum, frame):
        _stop.set()

    old_handler = signal.signal(signal.SIGINT, on_sigint)

    show_bars = not args.quiet and not args.dry_run and sys.stderr.isatty()
    nbars = args.jobs if show_bars else 0

    results: list[Result] = []
    slots: queue.Queue[int] = queue.Queue()
    for i in range(args.jobs):
        slots.put(i)

    overall = tqdm(
        total=len(images),
        position=0,
        unit="file",
        dynamic_ncols=True,
        disable=args.dry_run,
    )
    lock = threading.Lock()

    def worker(img: Path) -> None:
        slot = slots.get()
        try:
            if _stop.is_set():
                r = Result(image=img, skipped=True, reason="cancelled")
            else:
                r = process_one(img, args.mode, args, chdman, slot, nbars)
        except Exception as exc:  # noqa: BLE001
            r = Result(image=img, reason=f"{type(exc).__name__}: {exc}")
        finally:
            slots.put(slot)
        with lock:
            results.append(r)
            overall.update(1)
            if r.ok:
                pct = (r.dst_bytes / r.src_bytes * 100) if r.src_bytes else 0
                tqdm.write(f"  ok   {img.name} -> {human(r.dst_bytes)} ({pct:.1f}% of source)")
            elif r.skipped:
                tqdm.write(f"  skip {img.name}: {r.reason}")
            else:
                tqdm.write(f"  FAIL {img.name}: {r.reason}")
                for line in r.log[-5:]:
                    tqdm.write(f"       {line}")

    threads: list[threading.Thread] = []
    try:
        if args.jobs == 1:
            for img in images:
                worker(img)
                if _stop.is_set():
                    break
        else:
            pending = list(images)
            active: list[threading.Thread] = []
            while pending or active:
                active = [t for t in active if t.is_alive()]
                while pending and len(active) < args.jobs and not _stop.is_set():
                    t = threading.Thread(target=worker, args=(pending.pop(0),), daemon=True)
                    t.start()
                    active.append(t)
                    threads.append(t)
                if _stop.is_set():
                    pending.clear()
                for t in active:
                    t.join(timeout=0.2)
                    break
    finally:
        overall.close()
        signal.signal(signal.SIGINT, old_handler)
        for t in threads:
            t.join(timeout=5)

    ok = [r for r in results if r.ok]
    failed = [r for r in results if not r.ok and not r.skipped]
    skipped = [r for r in results if r.skipped]

    src = sum(r.src_bytes for r in ok)
    dst = sum(r.dst_bytes for r in ok)
    print(f"\nConverted {len(ok)}, skipped {len(skipped)}, failed {len(failed)}.")
    if ok:
        saved = src - dst
        pct = (saved / src * 100) if src else 0
        print(f"{human(src)} -> {human(dst)}  (saved {human(saved)}, {pct:.1f}%)")
    if _stop.is_set():
        print("Interrupted.")
        return 130
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())