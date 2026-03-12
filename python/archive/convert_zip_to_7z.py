#!/usr/bin/env python3
"""
Convert one or more ZIP archives to 7z archives with maximum compression.
Supports shell globbing and parallel processing: 
  python zip_to_7z.py *.zip --overwrite -v --jobs 4

Usage:
  python zip_to_7z.py <zip1.zip> [zip2.zip ...] [--overwrite] [-v | --verbose] [--jobs N]
"""

import argparse
import os
import shutil
import subprocess
import sys
import threading
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path


# Global lock for safe printing (avoids interleaved output in parallel mode)
_print_lock = threading.Lock()


def _print_safe(*args, **kwargs):
    """Thread-safe print wrapper."""
    with _print_lock:
        print(*args, **kwargs)


def get_temp_dir(zip_path: Path) -> Path:
    """Return the hidden temp directory path: e.g., .archive/ for archive.zip"""
    stem = zip_path.stem  # e.g., "archive" for "archive.zip"
    return zip_path.parent / f".{stem}"


def extract_zip_to_temp(zip_path: Path, temp_dir: Path, verbose: bool = False) -> list[Path]:
    """Extract zip contents to temp_dir and return list of extracted file paths (excluding dirs)."""
    if verbose:
        _print_safe(f"Extracting '{zip_path}' to '{temp_dir}'...")
    
    # Ensure temp_dir is clean
    if temp_dir.exists():
        shutil.rmtree(temp_dir)
    temp_dir.mkdir(parents=True, exist_ok=True)
    
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
    """Create 7z archive using maximum compression settings."""
    if verbose:
        _print_safe(f"Creating '{output_path}'...", file=sys.stderr)
    
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
    try:
        cmd = ["7z", "t", str(archive_path), "-y"]
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            raise RuntimeError(f"7z test failed: {result.stderr.strip()}")
        return True
    except Exception as e:
        _print_safe(f"Warning: Could not test '{archive_path}': {e}", file=sys.stderr)
        return False


def confirm_overwrite(output_path: Path, force: bool) -> bool:
    """Return True if it's safe to overwrite; False if user aborts."""
    if force:
        return True
    if not output_path.exists():
        return True
    response = input(f"'{output_path}' already exists. Overwrite? [y/N]: ").strip().lower()
    return response in ("y", "yes")


def convert_zip(
    zip_path: Path,
    overwrite: bool = False,
    verbose: bool = False,
    delete_source: bool = False,
    test_after: bool = False,
    keep_temp: bool = False
) -> tuple[bool, str]:
    """
    Convert a single ZIP to 7z. Returns (success: bool, message: str).
    Thread-safe output via _print_safe.
    """
    output_path = zip_path.with_suffix(".7z")
    
    # Check overwrite
    if not confirm_overwrite(output_path, overwrite):
        msg = f"Skipping '{zip_path}' (overwrite declined)."
        return False, msg
    
    temp_dir = get_temp_dir(zip_path)
    success = False
    message = ""
    
    try:
        # Extract to temp dir
        files = extract_zip_to_temp(zip_path, temp_dir, verbose)
        
        # Create 7z archive
        create_7z_archive(temp_dir, output_path, verbose)
        
        # Test if requested
        if test_after and not test_7z_archive(output_path, verbose):
            message = f"Failed integrity test: '{output_path}'"
            return False, message
        
        success = True
        message = f"Converted '{zip_path}' → '{output_path}'"
        
        # Delete source if requested and conversion succeeded
        if delete_source:
            try:
                zip_path.unlink()
                message = f"Converted '{zip_path}' → '{output_path}' (source deleted)"
            except Exception as e:
                _print_safe(f"Warning: Failed to delete '{zip_path}': {e}", file=sys.stderr)
        
        return success, message
        
    except Exception as e:
        message = f"Error '{zip_path}': {e}"
        return False, message
        
    finally:
        # Always clean up temp directory *unless* --keep-temp and failure
        if keep_temp and not success:
            _print_safe(f"Preserved temp dir '{temp_dir}' for debugging.", file=sys.stderr)
        elif temp_dir.exists():
            try:
                shutil.rmtree(temp_dir)
            except Exception as cleanup_error:
                _print_safe(f"Warning: Failed to clean up '{temp_dir}': {cleanup_error}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(
        description="Convert one or more ZIP archives to 7z archives with maximum compression.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Example:\n  python zip_to_7z.py *.zip --overwrite -v --jobs 4"
    )
    parser.add_argument(
        "zip_files",
        nargs="+",
        help="One or more ZIP files (supports shell globbing like *.zip)"
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
        _print_safe(f"Adjusted --jobs from {args.jobs} to {jobs} (capped to {cpu_count} CPUs).", file=sys.stderr)
    
    # Convert paths to Path objects and validate existence
    zip_paths = []
    for arg in args.zip_files:
        p = Path(arg).absolute()
        if not p.exists():
            _print_safe(f"Error: '{arg}' does not exist.", file=sys.stderr)
            continue
        if not p.is_file():
            _print_safe(f"Warning: '{arg}' is not a file — skipping.", file=sys.stderr)
            continue
        if p.suffix.lower() != ".zip":
            _print_safe(f"Warning: '{arg}' is not a .zip file — skipping.", file=sys.stderr)
            continue
        zip_paths.append(p)
    
    if not zip_paths:
        _print_safe("No valid ZIP files to process.", file=sys.stderr)
        sys.exit(1)
    
    total = len(zip_paths)
    
    # Parallel execution (use ThreadPoolExecutor for I/O-bound CLI subprocesses)
    results = []
    completed = 0
    
    # Use a lock for the shared counter to avoid race conditions
    counter_lock = threading.Lock()
    
    def process_and_record(zip_path: Path) -> tuple[bool, str]:
        nonlocal completed
        ok, msg = convert_zip(
            zip_path,
            overwrite=args.overwrite,
            verbose=args.verbose,
            delete_source=args.delete_source,
            test_after=args.test_after,
            keep_temp=args.keep_temp
        )
        with counter_lock:
            completed += 1
            # Print progress only when job completes (overall [N/total] done)
            progress_msg = f"[{completed}/{total}]"
            _print_safe(f"{progress_msg} {msg}")
        return ok, msg
    
    with ThreadPoolExecutor(max_workers=jobs) as executor:
        futures = [executor.submit(process_and_record, zip_path) for zip_path in zip_paths]
        # Collect results in order (for deterministic summary)
        for future in futures:
            results.append(future.result())
    
    # Summary (same as original)
    success_count = sum(1 for ok, _ in results if ok)
    
    if success_count == total:
        _print_safe(f"Converted {success_count}/{total} ZIP file(s) successfully.", file=sys.stderr)
    elif success_count > 0:
        _print_safe(f"Converted {success_count}/{total} ZIP file(s). {total - success_count} failed.", file=sys.stderr)
    else:
        _print_safe(f"No ZIP files were converted ({total} processed).", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
