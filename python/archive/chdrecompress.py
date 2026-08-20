#!/usr/bin/env python3
"""
chdrecompress.py -- re-run existing .chd files through chdman with maximum
compression settings and keep the result only if it actually came out smaller.

Examples:
    ./chdrecompress.py game.chd
    ./chdrecompress.py /roms/chd -r -j 4 --min-gain 1
    ./chdrecompress.py '/roms/*.chd' --no-replace --outdir /roms/recompressed
    ./chdrecompress.py /roms/chd --skip-optimal --dry-run
"""

from __future__ import annotations

import argparse
import csv
import glob
import os
import re
import shutil
import signal
import subprocess
import sys
import threading
import queue
from dataclasses import dataclass, field
from pathlib import Path

try:
    from tqdm import tqdm
except ImportError:
    sys.exit("This script requires tqdm.  Install it with:  pip install tqdm")


MODE_CFG = {
    "cd": {
        "unit": 2448,
        "unit_name": "frames",
        "default_units": 16,  # 39168 bytes
        "codecs": ["cdlz", "cdzl", "cdfl"],
    },
    "dvd": {
        "unit": 2048,
        "unit_name": "sectors",
        "default_units": 16,  # 32768 bytes
        "codecs": ["lzma", "zlib", "huff", "flac"],
    },
}

PROGRESS_RE = re.compile(
    r"(?P<verb>\w+),\s*(?P<pct>\d+(?:\.\d+)?)%\s*complete"
    r"(?:.*?ratio\s*=\s*(?P<ratio>\d+(?:\.\d+)?)%)?",
    re.IGNORECASE,
)
HUNK_RE = re.compile(r"Hunk\s+Size:\s*([\d,]+)", re.IGNORECASE)
COMP_RE = re.compile(r"Compression:\s*(.+)", re.IGNORECASE)
# CD/GD-ROM track metadata tags written by createcd / createdvd's CD cousins.
CD_TAGS = ("CHT2", "CHTR", "CHCD", "CHGT", "CHGD")

_stop = threading.Event()


class Cancelled(Exception):
    pass


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


def expand_inputs(patterns: list[str], recursive: bool) -> list[Path]:
    out: list[Path] = []
    seen: set[Path] = set()

    def add(p: Path) -> None:
        rp = p.resolve()
        if rp not in seen:
            seen.add(rp)
            out.append(rp)

    for pat in patterns:
        expanded = [Path(p) for p in glob.glob(os.path.expanduser(pat), recursive=True)]
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
                    if child.is_file() and child.suffix.lower() == ".chd":
                        add(child)
            elif path.is_file():
                add(path)
    return out


# --------------------------------------------------------------------------- #
# chdman info
# --------------------------------------------------------------------------- #


@dataclass
class ChdInfo:
    hunk_bytes: int | None = None
    codecs: list[str] = field(default_factory=list)
    is_cd: bool = False
    raw: str = ""


def probe(chdman: str, path: Path) -> ChdInfo:
    """Read hunk size, codec list and CD-ness out of 'chdman info'."""
    info = ChdInfo()
    try:
        proc = subprocess.run(
            [chdman, "info", "-i", str(path)],
            capture_output=True,
            text=True,
            errors="replace",
            timeout=120,
        )
    except (OSError, subprocess.TimeoutExpired):
        return info

    info.raw = (proc.stdout or "") + (proc.stderr or "")

    m = HUNK_RE.search(info.raw)
    if m:
        info.hunk_bytes = int(m.group(1).replace(",", ""))

    m = COMP_RE.search(info.raw)
    if m:
        for chunk in m.group(1).split(","):
            tag = chunk.strip().split()[0].strip() if chunk.strip() else ""
            tag = tag.split("(")[0].strip()
            if tag and tag.lower() not in ("none",):
                info.codecs.append(tag)

    if any(tag in info.raw for tag in CD_TAGS) or "cdrom" in info.raw.lower():
        info.is_cd = True
    elif any(c.startswith("cd") for c in info.codecs):
        info.is_cd = True
    elif info.hunk_bytes and info.hunk_bytes % 2448 == 0 and info.hunk_bytes % 2048:
        # Divisible by the CD frame size but not the DVD sector size.
        info.is_cd = True
    return info


# --------------------------------------------------------------------------- #
# Running chdman
# --------------------------------------------------------------------------- #


def run_chdman(cmd: list[str], bar: tqdm | None, label: str) -> tuple[int, list[str]]:
    tail: list[str] = []
    kwargs: dict = {}
    if os.name == "nt":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        kwargs["start_new_session"] = True

    proc = subprocess.Popen(
        cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, bufsize=0, **kwargs
    )

    def handle(line: str) -> None:
        m = PROGRESS_RE.search(line)
        if m and bar is not None:
            bar.n = min(float(m.group("pct")), 100.0)
            ratio = m.group("ratio")
            suffix = f" ratio={ratio}%" if ratio else ""
            bar.set_description_str(f"  {m.group('verb').capitalize()} {label}{suffix}"[:70])
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
        except Exception:
            proc.kill()
        raise
    finally:
        try:
            proc.stderr.close()
        except Exception:
            pass


# --------------------------------------------------------------------------- #
# Per-file work
# --------------------------------------------------------------------------- #


@dataclass
class Result:
    path: Path
    ok: bool = False
    skipped: bool = False
    replaced: bool = False
    reason: str = ""
    mode: str = ""
    old_bytes: int = 0
    new_bytes: int = 0
    old_hunk: int | None = None
    old_codecs: list[str] = field(default_factory=list)
    log: list[str] = field(default_factory=list)


def target_settings(mode: str, args: argparse.Namespace) -> tuple[list[str], int]:
    cfg = MODE_CFG[mode]
    codecs = args.codec_list or list(cfg["codecs"])
    if args.hunk_size is not None:
        hunk = args.hunk_size
    else:
        units = args.units if args.units is not None else cfg["default_units"]
        hunk = units * cfg["unit"]
    return codecs, hunk


def process_one(
    path: Path, args: argparse.Namespace, chdman: str, slot: int, nbars: int
) -> Result:
    res = Result(path=path)
    res.old_bytes = path.stat().st_size

    info = probe(chdman, path)
    res.old_hunk = info.hunk_bytes
    res.old_codecs = info.codecs

    if args.mode == "auto":
        mode = "cd" if info.is_cd else "dvd"
    else:
        mode = args.mode
    res.mode = mode

    codecs, hunk = target_settings(mode, args)
    unit = MODE_CFG[mode]["unit"]
    if hunk % unit:
        res.reason = f"hunk size {hunk} is not a multiple of {unit} for {mode} images"
        return res

    if args.skip_optimal and info.hunk_bytes == hunk and info.codecs == codecs:
        res.skipped = True
        res.reason = f"already {','.join(codecs)} @ {hunk}"
        return res

    outdir = Path(args.outdir) if args.outdir else path.parent
    replace = args.outdir is None and not args.no_replace
    if replace:
        final = path
        tmp = path.with_name(path.stem + ".recomp.tmp.chd")
    else:
        final = outdir / (path.stem + args.suffix + ".chd")
        tmp = outdir / (path.stem + ".recomp.tmp.chd")
        if final.exists() and not args.force:
            res.skipped = True
            res.reason = "output exists (use --force)"
            return res
        if final.resolve() == path.resolve():
            res.skipped = True
            res.reason = "output would overwrite the input (set --suffix or --outdir)"
            return res

    cmd = [
        chdman, "copy",
        "-i", str(path),
        "-o", str(tmp),
        "-c", ",".join(codecs),
        "-hs", str(hunk),
        "-f",
    ]
    if args.cores:
        cmd += ["--numprocessors", str(args.cores)]
    cmd += args.extra

    if args.dry_run:
        res.skipped = True
        res.reason = "dry run: " + " ".join(cmd)
        return res

    outdir.mkdir(parents=True, exist_ok=True)

    bar = None
    if nbars:
        bar = tqdm(
            total=100, position=slot + 1, leave=False,
            bar_format="{desc} |{bar}| {n:.1f}%", dynamic_ncols=True,
        )
    try:
        rc, tail = run_chdman(cmd, bar, path.name)
        if rc != 0:
            res.reason = f"chdman copy exited {rc}"
            res.log = tail
            tmp.unlink(missing_ok=True)
            return res

        res.new_bytes = tmp.stat().st_size
        gain = res.old_bytes - res.new_bytes
        gain_pct = (gain / res.old_bytes * 100) if res.old_bytes else 0.0

        if gain_pct < args.min_gain and not args.keep_larger:
            tmp.unlink(missing_ok=True)
            res.ok = True
            res.reason = (
                f"no worthwhile gain ({gain_pct:+.2f}%)"
                if gain > 0
                else f"already better ({gain_pct:+.2f}%)"
            )
            res.new_bytes = res.old_bytes
            return res

        if args.verify:
            if bar is not None:
                bar.reset(total=100)
            rc, tail = run_chdman([chdman, "verify", "-i", str(tmp)], bar, path.name)
            if rc != 0:
                res.reason = f"verification failed (chdman exited {rc})"
                res.log = tail
                tmp.unlink(missing_ok=True)
                return res

        if replace:
            if args.backup:
                backup = path.with_suffix(path.suffix + ".bak")
                if backup.exists() and not args.force:
                    tmp.unlink(missing_ok=True)
                    res.reason = f"backup {backup.name} already exists"
                    return res
                shutil.move(str(path), str(backup))
            os.replace(tmp, final)
            res.replaced = True
        else:
            os.replace(tmp, final)

        res.ok = True
        res.reason = f"{gain_pct:+.2f}%"
        return res
    except Cancelled:
        tmp.unlink(missing_ok=True)
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
        description="Recompress existing .chd files with maximum compression, "
        "keeping the result only if it is smaller.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""\
By default each file is re-encoded to a temporary file next to the original,
verified, and then moved over the original only if it came out smaller.  Use
--no-replace (optionally with --outdir / --suffix) to keep both copies, or
--backup to leave the previous version as <name>.chd.bak.

--skip-optimal uses 'chdman info' to skip files whose codec list and hunk size
already match the target, which is much faster than re-encoding everything.
""",
    )
    p.add_argument("inputs", nargs="+", metavar="INPUT", help="file, directory, or glob pattern")

    p.add_argument("--chdman", metavar="PATH", help="path to the chdman binary")
    p.add_argument("--mode", choices=("auto", "cd", "dvd"), default="auto",
                   help="which defaults to apply (default: detect from chdman info)")
    p.add_argument("-o", "--outdir", metavar="DIR",
                   help="write results here instead of replacing the originals")
    p.add_argument("--suffix", default=".max", metavar="STR",
                   help="filename suffix when not replacing (default: .max)")
    p.add_argument("-r", "--recursive", action="store_true", help="recurse into subdirectories")

    size = p.add_mutually_exclusive_group()
    size.add_argument("--hunk-size", type=int, metavar="BYTES", help="target hunk size in bytes")
    size.add_argument("--frames", "--sectors", dest="units", type=int, metavar="N",
                      help="target hunk size in CD frames (2448 B) or DVD sectors (2048 B)")

    p.add_argument("-c", "--cores", type=int, metavar="N",
                   help="cores per chdman process (default: all, split across --jobs)")
    p.add_argument("-j", "--jobs", type=int, default=1, metavar="N",
                   help="files to process in parallel (default: 1)")
    p.add_argument("--codecs", metavar="LIST", help="override the codec list")

    p.add_argument("--min-gain", type=float, default=0.0, metavar="PCT",
                   help="only keep the new file if it saves at least this %% (default: 0)")
    p.add_argument("--keep-larger", action="store_true",
                   help="keep the re-encoded file even if it is not smaller")
    p.add_argument("--skip-optimal", action="store_true",
                   help="skip files already using the target codecs and hunk size")
    p.add_argument("--no-replace", action="store_true",
                   help="never overwrite the original, write a second file instead")
    p.add_argument("--backup", action="store_true",
                   help="keep the original as <name>.chd.bak when replacing")

    p.add_argument("--no-verify", dest="verify", action="store_false", default=True,
                   help="skip the verification pass before keeping a result")
    p.add_argument("-f", "--force", action="store_true", help="overwrite existing outputs/backups")
    p.add_argument("-n", "--dry-run", action="store_true", help="print what would happen")
    p.add_argument("--report", metavar="CSV", help="write a per-file summary to this CSV")
    p.add_argument("-q", "--quiet", action="store_true", help="suppress per-file progress bars")
    p.add_argument("--extra", nargs=argparse.REMAINDER, default=[],
                   help="everything after this is passed straight to chdman")

    args = p.parse_args(argv)

    if args.hunk_size is not None and args.hunk_size <= 0:
        p.error("--hunk-size must be positive")
    if args.units is not None and args.units <= 0:
        p.error("--frames must be positive")

    cpu = os.cpu_count() or 1
    if args.jobs < 1:
        p.error("--jobs must be >= 1")
    if args.cores is None:
        args.cores = max(1, cpu // args.jobs)
    elif args.cores < 1:
        p.error("--cores must be >= 1")

    args.codec_list = (
        [c.strip() for c in args.codecs.split(",") if c.strip()] if args.codecs else None
    )
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    chdman = resolve_chdman(args.chdman)

    files = expand_inputs(args.inputs, args.recursive)
    files = [f for f in files if f.suffix.lower() == ".chd"]
    if not files:
        print("Nothing to do: no .chd files found.", file=sys.stderr)
        return 1

    print(f"chdman:  {chdman}")
    print(f"mode:    {args.mode}")
    if args.codec_list:
        print(f"codecs:  {','.join(args.codec_list)}")
    else:
        print(f"codecs:  cd={','.join(MODE_CFG['cd']['codecs'])}  dvd={','.join(MODE_CFG['dvd']['codecs'])}")
    if args.hunk_size is not None:
        print(f"hunk:    {args.hunk_size} bytes")
    else:
        u = args.units if args.units is not None else None
        cd_u = u or MODE_CFG["cd"]["default_units"]
        dvd_u = u or MODE_CFG["dvd"]["default_units"]
        print(f"hunk:    cd={cd_u * 2448}  dvd={dvd_u * 2048}")
    print(f"jobs:    {args.jobs} x {args.cores} core(s)")
    print(f"files:   {len(files)}\n")

    signal.signal(signal.SIGINT, lambda *_: _stop.set())

    show_bars = not args.quiet and not args.dry_run and sys.stderr.isatty()
    nbars = args.jobs if show_bars else 0

    results: list[Result] = []
    slots: queue.Queue[int] = queue.Queue()
    for i in range(args.jobs):
        slots.put(i)

    overall = tqdm(total=len(files), position=0, unit="file", dynamic_ncols=True,
                   disable=args.dry_run)
    lock = threading.Lock()

    def worker(path: Path) -> None:
        slot = slots.get()
        try:
            if _stop.is_set():
                r = Result(path=path, skipped=True, reason="cancelled")
            else:
                r = process_one(path, args, chdman, slot, nbars)
        except Exception as exc:  # noqa: BLE001
            r = Result(path=path, reason=f"{type(exc).__name__}: {exc}")
        finally:
            slots.put(slot)
        with lock:
            results.append(r)
            overall.update(1)
            if r.skipped:
                tqdm.write(f"  skip {path.name}: {r.reason}")
            elif r.ok and r.new_bytes < r.old_bytes:
                tqdm.write(
                    f"  ok   {path.name} [{r.mode}] {human(r.old_bytes)} -> "
                    f"{human(r.new_bytes)} ({r.reason})"
                )
            elif r.ok:
                tqdm.write(f"  keep {path.name} [{r.mode}]: {r.reason}")
            else:
                tqdm.write(f"  FAIL {path.name}: {r.reason}")
                for line in r.log[-5:]:
                    tqdm.write(f"       {line}")

    threads: list[threading.Thread] = []
    try:
        if args.jobs == 1:
            for f in files:
                worker(f)
                if _stop.is_set():
                    break
        else:
            pending = list(files)
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
        for t in threads:
            t.join(timeout=5)

    ok = [r for r in results if r.ok]
    shrunk = [r for r in ok if r.new_bytes < r.old_bytes]
    failed = [r for r in results if not r.ok and not r.skipped]
    skipped = [r for r in results if r.skipped]

    old = sum(r.old_bytes for r in ok)
    new = sum(r.new_bytes for r in ok)
    print(
        f"\nProcessed {len(ok)} ({len(shrunk)} improved), "
        f"skipped {len(skipped)}, failed {len(failed)}."
    )
    if ok:
        saved = old - new
        pct = (saved / old * 100) if old else 0
        print(f"{human(old)} -> {human(new)}  (saved {human(saved)}, {pct:.2f}%)")

    if args.report and results:
        with open(args.report, "w", newline="", encoding="utf-8") as fh:
            w = csv.writer(fh)
            w.writerow([
                "file", "mode", "status", "old_bytes", "new_bytes", "saved_bytes",
                "saved_pct", "old_hunk", "old_codecs", "note",
            ])
            for r in sorted(results, key=lambda x: str(x.path)):
                saved = r.old_bytes - r.new_bytes if r.ok else 0
                pct = (saved / r.old_bytes * 100) if r.ok and r.old_bytes else 0
                status = "skipped" if r.skipped else ("ok" if r.ok else "failed")
                w.writerow([
                    r.path, r.mode, status, r.old_bytes, r.new_bytes or "", saved,
                    f"{pct:.2f}", r.old_hunk or "", ",".join(r.old_codecs), r.reason,
                ])
        print(f"Report written to {args.report}")

    if _stop.is_set():
        print("Interrupted.")
        return 130
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())