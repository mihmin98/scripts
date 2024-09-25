import subprocess
from pathlib import Path
import argparse
import sys

if __name__ == '__main__':
    if len(sys.argv) != 2:
        print('bad number of args')

    if sys.argv[1].endswith('"'):
        src_dir = Path(sys.argv[1][:-1])
    else:
        src_dir = Path(sys.argv[1])

    dest_dir = src_dir / 'output_opus'
    dest_dir.mkdir(exist_ok=True)

    flac_files = list(src_dir.glob('*.flac'))

    for flac_file in flac_files:
        output_path = dest_dir / flac_file.name.replace('.flac', '.opus')
        cmd = ['ffmpeg', '-i', str(flac_file), '-c:v', 'libtheora', '-q:v', '10', '-c:a', 'libopus', '-b:a', '160k', str(output_path)]
        print(' '.join(cmd))
        subprocess.run(cmd, text=True)