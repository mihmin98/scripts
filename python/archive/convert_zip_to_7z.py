#!/usr/bin/env python3
"""
Convert one or more ZIP archives to 7z archives with maximum compression.
Supports shell globbing and parallel processing:
  python convert_zip_to_7z.py *.zip --overwrite -v --jobs 4

Usage:
  python convert_zip_to_7z.py <zip1.zip> [zip2.zip ...] [--overwrite] [-v | --verbose] [--jobs N]
"""

import argparse
import glob
import os
import shutil
import subprocess
import sys
import tempfile
import threading
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import NamedTuple


# Global lock for safe printing (avoids interleaved output in parallel mode)
_print_lock = threading.Lock()


def _print_safe(*args, **kwargs):
    """Thread-safe print wrapper. Defaults to stderr so all status output is consistent."""
    kwargs.setdefault("file", sys.stderr)
    with _print_lock:
        print(*args, **kwargs)


class Result(NamedTuple):
    """Outcome of one conversion. Sizes are 0 for failures, so they sum harmlessly."""
    ok: bool
    message: str
    zip_size: int = 0
    out_size: int = 0


def format_size(num_bytes: int) -> str:
    """Format a byte count as a human-readable string."""
    size = float(num_bytes)
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if abs(size) < 1024.0 or unit == "TiB":
            return f"{size:.0f} {unit}" if unit == "B" else f"{size:.2f} {unit}"
        size /= 1024.0


def format_size_change(old_bytes: int, new_bytes: int) -> str:
    """Describe the size change from zip to 7z, e.g. '4.20 MiB → 3.10 MiB (-26.2%, saved 1.10 MiB)'."""
    delta = new_bytes - old_bytes
    summary = f"{format_size(old_bytes)} → {format_size(new_bytes)}"
    if old_bytes == 0:
        return summary
    pct = delta / old_bytes * 100
    verb = "saved" if delta <= 0 else "grew by"
    return f"{summary} ({pct:+.1f}%, {verb} {format_size(abs(delta))})"


def make_temp_dir(zip_path: Path) -> Path:
    """Create and return a unique hidden temp directory next to the zip.

    Unique per call so we never touch a pre-existing directory and never collide
    with another job (e.g. 'foo.zip' and 'foo.Zip' share a stem).
    """
    return Path(tempfile.mkdtemp(dir=zip_path.parent, prefix=f".{zip_path.stem}-"))


def extract_zip_to_temp(zip_path: Path, temp_dir: Path, verbose: bool = False) -> list[Path]:
    """Extract zip contents to temp_dir and return list of extracted file paths (excluding dirs)."""
    if verbose:
        _print_safe(f"Extracting '{zip_path}' to '{temp_dir}'...")

    try:
        cmd = ["7z", "x", str(zip_path), f"-o{temp_dir}", "-y"]
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            raise RuntimeError(f"7z extraction failed: {result.stderr.strip()}")
    except FileNotFoundError:
        raise RuntimeError("7z command not found. Ensure 7-Zip is installed and in PATH.")

    # Collect *files* (not directories)
    files = [p for p in temp_dir.rglob("*") if p.is_file()]
    if verbose:
        _print_safe(f"Extracted {len(files)} file(s).")

    return files


def create_7z_archive(temp_dir: Path, output_path: Path, verbose: bool = False) -> None:
    """Create 7z archive using maximum compression settings.

    Any existing archive is removed first: '7z a' *adds to* an existing archive,
    which would leave stale entries from a previous run in the output.
    """
    if verbose:
        _print_safe(f"Creating '{output_path}'...")

    output_path.unlink(missing_ok=True)

    cmd = [
        "7z", "a",
        "-t7z",
        "-mx9",
        "-myx9",
        "-m0=LZMA2:d1536m:fb64",
        "-ms=1t",
        "-mmt=2",
        "-mqs",
        "-slp",
        str(output_path),
        "*"
    ]

    try:
        result = subprocess.run(
            cmd,
            cwd=temp_dir,
            capture_output=True,
            text=True
        )
        if result.returncode != 0:
            raise RuntimeError(f"7z creation failed: {result.stderr.strip()}")
    except FileNotFoundError:
        raise RuntimeError("7z command not found. Ensure 7-Zip is installed and in PATH.")


def test_7z_archive(archive_path: Path, verbose: bool = False) -> bool:
    """Test integrity of 7z archive. Returns True if OK."""
    if verbose:
        _print_safe(f"Testing '{archive_path}'...")
    try:
        cmd = ["7z", "t", str(archive_path), "-y"]
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            _print_safe(f"Warning: integrity test failed for '{archive_path}': {result.stderr.strip()}")
            return False
        return True
    except OSError as e:
        _print_safe(f"Warning: Could not test '{archive_path}': {e}")
        return False


def confirm_overwrite(output_path: Path, force: bool) -> bool:
    """Return True if it's safe to overwrite; False if user aborts.

    Must be called from the main thread only (prompts cannot be interleaved).
    """
    if force:
        return True
    if not output_path.exists():
        return True
    try:
        response = input(f"'{output_path}' already exists. Overwrite? [y/N]: ").strip().lower()
    except EOFError:
        _print_safe(f"No input available; skipping '{output_path}' (use --overwrite to force).")
        return False
    return response in ("y", "yes")


def convert_zip(
    zip_path: Path,
    verbose: bool = False,
    delete_source: bool = False,
    test_after: bool = False,
    keep_temp: bool = False
) -> "Result":
    """
    Convert a single ZIP to 7z. Returns a Result (sizes are 0 on failure).
    Overwrite confirmation is expected to have happened already, in the main thread.
    Thread-safe output via _print_safe.
    """
    output_path = zip_path.with_suffix(".7z")
    temp_dir = None
    success = False

    try:
        # Measured up front: with --delete-source the zip is gone by the time we report.
        zip_size = zip_path.stat().st_size

        temp_dir = make_temp_dir(zip_path)

        # Extract to temp dir
        files = extract_zip_to_temp(zip_path, temp_dir, verbose)
        if not files:
            return Result(False, f"Error '{zip_path}': archive extracted 0 files, nothing to compress.")

        # Create 7z archive
        create_7z_archive(temp_dir, output_path, verbose)

        # Test if requested
        if test_after and not test_7z_archive(output_path, verbose):
            return Result(False, f"Failed integrity test: '{output_path}'")

        out_size = output_path.stat().st_size
        if verbose:
            _print_safe(f"'{zip_path.name}': {format_size_change(zip_size, out_size)}")

        success = True
        message = f"Converted '{zip_path}' → '{output_path}'"

        # Delete source if requested and conversion succeeded
        if delete_source:
            try:
                zip_path.unlink()
                message = f"Converted '{zip_path}' → '{output_path}' (source deleted)"
            except OSError as e:
                _print_safe(f"Warning: Failed to delete '{zip_path}': {e}")

        return Result(True, message, zip_size, out_size)

    except Exception as e:
        return Result(False, f"Error '{zip_path}': {e}")

    finally:
        # Always clean up temp directory *unless* --keep-temp and failure
        if temp_dir is not None and temp_dir.exists():
            if keep_temp and not success:
                _print_safe(f"Preserved temp dir '{temp_dir}' for debugging.")
            else:
                try:
                    shutil.rmtree(temp_dir)
                except OSError as cleanup_error:
                    _print_safe(f"Warning: Failed to clean up '{temp_dir}': {cleanup_error}")


def main():
    parser = argparse.ArgumentParser(
        description="Convert one or more ZIP archives to 7z archives with maximum compression.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Example:\n  python convert_zip_to_7z.py *.zip --overwrite -v --jobs 4"
    )
    parser.add_argument(
        "zip_files",
        nargs="+",
        help="One or more ZIP files or glob patterns. Unquoted patterns are expanded "
             "by the shell; quoted ones (needed when the path contains spaces) are "
             "expanded by this script, e.g. '/path/with space/*.zip'"
    )
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Enable verbose output (shows extraction/creation steps)"
    )
    parser.add_argument(
        "--overwrite",
        action="store_true",
        help="Overwrite existing .7z files without prompting"
    )
    parser.add_argument(
        "--jobs", type=int, default=1,
        help="Number of parallel jobs (default: 1, capped to CPU count)"
    )
    parser.add_argument(
        "--delete-source",
        action="store_true",
        help="Delete original ZIP after successful conversion"
    )
    parser.add_argument(
        "--test-after",
        action="store_true",
        help="Test 7z archive integrity after creation"
    )
    parser.add_argument(
        "--keep-temp",
        action="store_true",
        help="Keep temp dir on failure (for debugging)"
    )

    args = parser.parse_args()

    # Validate and cap jobs
    cpu_count = os.cpu_count() or 1
    jobs = max(1, min(args.jobs, cpu_count))
    if args.jobs != jobs:
        _print_safe(f"Adjusted --jobs from {args.jobs} to {jobs} (capped to {cpu_count} CPUs).")

    # Expand any wildcards ourselves, so quoted patterns work too. Quoting is often
    # unavoidable when the path contains spaces, and then the shell never globs.
    expanded_args = []
    invalid_count = 0
    for arg in args.zip_files:
        if glob.has_magic(arg):
            matches = sorted(glob.glob(os.path.expanduser(arg), recursive=True))
            if not matches:
                _print_safe(f"Error: pattern '{arg}' matched no files.")
                invalid_count += 1
                continue
            expanded_args.extend(matches)
        else:
            expanded_args.append(os.path.expanduser(arg))

    # Convert paths to Path objects and validate existence
    zip_paths = []
    for arg in expanded_args:
        p = Path(arg).absolute()
        if not p.exists():
            _print_safe(f"Error: '{arg}' does not exist.")
            invalid_count += 1
            continue
        if not p.is_file():
            _print_safe(f"Warning: '{arg}' is not a file — skipping.")
            invalid_count += 1
            continue
        if p.suffix.lower() != ".zip":
            _print_safe(f"Warning: '{arg}' is not a .zip file — skipping.")
            invalid_count += 1
            continue
        zip_paths.append(p)

    if not zip_paths:
        _print_safe("No valid ZIP files to process.")
        sys.exit(1)

    # Resolve overwrite prompts serially, before starting any workers: input() from
    # several threads at once produces interleaved prompts and misdirected answers.
    declined_count = 0
    confirmed_paths = []
    for p in zip_paths:
        if confirm_overwrite(p.with_suffix(".7z"), args.overwrite):
            confirmed_paths.append(p)
        else:
            declined_count += 1
            _print_safe(f"Skipping '{p}' (overwrite declined).")
    zip_paths = confirmed_paths

    if not zip_paths:
        _print_safe("Nothing to do — all files were skipped.")
        sys.exit(1 if invalid_count else 0)

    total = len(zip_paths)

    # Parallel execution (use ThreadPoolExecutor for I/O-bound CLI subprocesses)
    results = []
    completed = 0

    # Use a lock for the shared counter to avoid race conditions
    counter_lock = threading.Lock()

    def process_and_record(zip_path: Path) -> Result:
        nonlocal completed
        result = convert_zip(
            zip_path,
            verbose=args.verbose,
            delete_source=args.delete_source,
            test_after=args.test_after,
            keep_temp=args.keep_temp
        )
        with counter_lock:
            completed += 1
            # Print progress only when job completes (overall [N/total] done)
            progress_msg = f"[{completed}/{total}]"
            _print_safe(f"{progress_msg} {result.message}")
        return result

    with ThreadPoolExecutor(max_workers=jobs) as executor:
        futures = [executor.submit(process_and_record, zip_path) for zip_path in zip_paths]
        # Collect results in order (for deterministic summary)
        for future in futures:
            results.append(future.result())

    # Summary
    success_count = sum(1 for r in results if r.ok)

    if success_count == total:
        _print_safe(f"Converted {success_count}/{total} ZIP file(s) successfully.")
    elif success_count > 0:
        _print_safe(f"Converted {success_count}/{total} ZIP file(s). {total - success_count} failed.")
    else:
        _print_safe(f"No ZIP files were converted ({total} processed).")

    # Queue totals across everything that converted successfully
    if success_count:
        total_zip = sum(r.zip_size for r in results)
        total_out = sum(r.out_size for r in results)
        _print_safe(f"Total: {format_size_change(total_zip, total_out)}")

    if invalid_count:
        _print_safe(f"{invalid_count} argument(s) were invalid and not processed.")
    if declined_count:
        _print_safe(f"{declined_count} file(s) skipped (overwrite declined).")

    if success_count < total or invalid_count:
        sys.exit(1)


if __name__ == "__main__":
    main()
