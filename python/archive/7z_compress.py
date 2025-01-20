import subprocess
from pathlib import Path
import argparse

def main():
    parser = argparse.ArgumentParser(description='Used to compress fles with a given exten into separate 7z archives')
    parser.add_argument('dir', help='Directory which contains the files')
    parser.add_argument('--ext', '--extension', help='Extension of the files without leading dot (e.g. gba, gbc)', required=True)

    args = parser.parse_args()

    src_dir = Path(args.dir).absolute()

    files = list(src_dir.glob(f'*.{args.ext}'))
    for f in files:
        archive_path = f.parent / f.name.replace(f'.{args.ext}', '.7z')
        # cmd = ['7z', 'a', str(archive_path), str(f), '-mx9']
        cmd = ['7z', 'a', str(archive_path), str(f), '-mx9', '-myx9', '-m0=LZMA2:d1536m:fb64', '-ms=1t', '-mmt=2', '-mqs', '-slp']
        print(' '.join(cmd))
        subprocess.run(cmd, text=True)

if __name__ == '__main__':
    main()
