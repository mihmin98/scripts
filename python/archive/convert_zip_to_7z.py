#!/usr/bin/env python3
"""
Convert one or more ZIP archives to 7z archives with maximum compression.
Supports shell globbing: python zip_to_7z.py *.zip --overwrite -v

Usage:
  python zip_to_7z.py <zip1.zip> [zip2.zip ...] [--overwrite] [-v | --verbose]
"""

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path


def get_temp_dir(zip_path: Path) -> Path:
    """Return the hidden temp directory path: e.g., .archive/ for archive.zip"""
    stem = zip_path.stem  # e.g., "archive" for "archive.zip"
    return zip_path.parent / f".{stem}"


def extract_zip_to_temp(zip_path: Path, temp_dir: Path, verbose: bool = False) -> list[Path]:
    """Extract zip contents to temp_dir and return list of extracted file paths (excluding dirs)."""
    if verbose:
        print(f"Extracting '{zip_path}' to '{temp_dir}'...")
    
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
        print(f"Extracted {len(files)} file(s).")
    
    return files


def create_7z_archive(temp_dir: Path, output_path: Path, verbose: bool = False) -> None:
    """Create 7z archive using maximum compression settings."""
    if verbose:
        print(f"Creating '{output_path}'...", file=sys.stderr)
    
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
    verbose: bool = False
) -> bool:
    """Convert a single ZIP to 7z. Returns True on success, False on error."""
    output_path = zip_path.with_suffix(".7z")
    
    # Check overwrite
    if not confirm_overwrite(output_path, overwrite):
        print(f"Skipping '{zip_path}' (overwrite declined).", file=sys.stderr)
        return False
    
    temp_dir = get_temp_dir(zip_path)
    
    try:
        # Extract to temp dir
        files = extract_zip_to_temp(zip_path, temp_dir, verbose)
        
        # Create 7z archive
        create_7z_archive(temp_dir, output_path, verbose)
        
        return True
        
    except Exception as e:
        print(f"Error converting '{zip_path}': {e}", file=sys.stderr)
        return False
        
    finally:
        # Always clean up temp directory
        if temp_dir.exists():
            try:
                shutil.rmtree(temp_dir)
            except Exception as cleanup_error:
                print(f"Warning: Failed to clean up '{temp_dir}': {cleanup_error}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(
        description="Convert one or more ZIP archives to 7z archives with maximum compression.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Example:\n  python zip_to_7z.py *.zip --overwrite -v"
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
    
    args = parser.parse_args()
    
    # Convert paths to Path objects and validate existence
    zip_paths = []
    for arg in args.zip_files:
        p = Path(arg).absolute()
        if not p.exists():
            print(f"Error: '{arg}' does not exist.", file=sys.stderr)
            continue
        if not p.is_file():
            print(f"Warning: '{arg}' is not a file — skipping.", file=sys.stderr)
            continue
        if p.suffix.lower() != ".zip":
            print(f"Warning: '{arg}' is not a .zip file — skipping.", file=sys.stderr)
            continue
        zip_paths.append(p)
    
    if not zip_paths:
        print("No valid ZIP files to process.", file=sys.stderr)
        sys.exit(1)
    
    total = len(zip_paths)
    success_count = 0
    
    # Progress display
    for i, zip_path in enumerate(zip_paths, start=1):
        # Clear line and show progress
        terminal_columns = os.get_terminal_size().columns
        progress_msg = f"\rProgress: [{i}/{total}] converting '{zip_path.name}'"
        if len(progress_msg) < terminal_columns:
            diff = terminal_columns - len(progress_msg)
            progress_msg += ' ' * diff
        sys.stdout.write(progress_msg)
        sys.stdout.flush()
        
        # Do the conversion
        ok = convert_zip(zip_path, overwrite=args.overwrite, verbose=args.verbose)
        if ok:
            success_count += 1
    
    # Final newline after progress line
    sys.stdout.write("\n")
    sys.stdout.flush()
    
    # Summary
    if success_count == total:
        print(f"Converted {success_count}/{total} ZIP file(s) successfully.", file=sys.stderr)
    elif success_count > 0:
        print(f"Converted {success_count}/{total} ZIP file(s). {total - success_count} failed.", file=sys.stderr)
    else:
        print(f"No ZIP files were converted ({total} processed).", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
