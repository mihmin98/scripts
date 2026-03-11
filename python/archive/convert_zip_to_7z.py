#!/usr/bin/env python3
"""
Convert a ZIP archive to a 7z archive with maximum compression settings.
Usage: python zip_to_7z.py <input.zip> [--overwrite] [-v | --verbose]
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
    
    # Ensure temp_dir is clean (remove if exists, then create)
    if temp_dir.exists():
        shutil.rmtree(temp_dir)
    temp_dir.mkdir(parents=True, exist_ok=True)
    
    # Use 7z to extract (more reliable for large/complex archives)
    try:
        cmd = ["7z", "x", str(zip_path), f"-o{temp_dir}", "-y"]
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            raise RuntimeError(f"7z extraction failed: {result.stderr.strip()}")
    except FileNotFoundError:
        raise RuntimeError("7z command not found. Ensure 7-Zip is installed and in PATH.")
    
    # Collect *files* (not directories) recursively
    files = [p for p in temp_dir.rglob("*") if p.is_file()]
    if verbose:
        print(f"Extracted {len(files)} file(s).")
    
    return files


def create_7z_archive(temp_dir: Path, output_path: Path, verbose: bool = False) -> None:
    """Create 7z archive using maximum compression settings."""
    if verbose:
        print(f"Creating '{output_path}' with maximum compression...")
    
    # Build 7z command with provided compression args
    # Note: -slp suppresses line buffering (improves performance), -ms=1t enables solid block
    # -mqs: query solid blocks for large archives
    # -mmt=2: 2 threads; change to -mmt=on for auto-detection
    # -mx9: maximum compression level; -myx9: maximum compression for solid blocks
    # -m0=LZMA2:d1536m:fb64: LZMA2 with 1.5 GB dictionary & 64-byte fast bytes
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
        "*"  # Add all contents of temp_dir
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
        if verbose:
            print(f"7z archive created: {output_path}")
    except FileNotFoundError:
        raise RuntimeError("7z command not found. Ensure 7-Zip is installed and in PATH.")


def main():
    parser = argparse.ArgumentParser(
        description="Convert a ZIP archive to a 7z archive with maximum compression.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Example: python zip_to_7z.py archive.zip --overwrite -v"
    )
    parser.add_argument("zip_file", help="Input ZIP file to convert")
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Enable verbose output"
    )
    parser.add_argument(
        "--overwrite",
        action="store_true",
        help="Overwrite existing .7z file without prompting"
    )
    
    args = parser.parse_args()
    
    zip_path = Path(args.zip_file).absolute()
    
    # Ensure input exists and is a file
    if not zip_path.exists():
        print(f"Error: Input file '{zip_path}' does not exist.", file=sys.stderr)
        sys.exit(1)
    if not zip_path.is_file():
        print(f"Error: '{zip_path}' is not a file.", file=sys.stderr)
        sys.exit(1)
    
    # Derive output path and temp directory
    output_path = zip_path.with_suffix(".7z")
    temp_dir = get_temp_dir(zip_path)
    
    # Check for existing .7z and prompt (unless --overwrite)
    if output_path.exists() and not args.overwrite:
        response = input(f"'{output_path}' already exists. Overwrite? [y/N]: ").strip().lower()
        if response not in ("y", "yes"):
            print("Aborted.")
            sys.exit(0)
    
    try:
        # Step 1: Extract ZIP to temp dir
        files = extract_zip_to_temp(zip_path, temp_dir, args.verbose)
        
        # Step 2: Create 7z archive from temp dir contents
        # Even if files is empty, create archive (7z handles it)
        create_7z_archive(temp_dir, output_path, args.verbose)
        
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        # Exit with error, but cleanup will happen in finally block
        sys.exit(1)
    
    finally:
        # Always clean up temp directory, even on error
        if temp_dir.exists():
            if args.verbose:
                print(f"Cleaning up temp directory '{temp_dir}'...")
            try:
                shutil.rmtree(temp_dir)
            except Exception as cleanup_error:
                print(f"Warning: Failed to clean up '{temp_dir}': {cleanup_error}", file=sys.stderr)


if __name__ == "__main__":
    main()
