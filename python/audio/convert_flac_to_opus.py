import subprocess
from pathlib import Path
import argparse

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('music_dir', type=str, help='Directory which contains the flac files')
    parser.add_argument('--parent_dir_name', action='store_true', help='Use the parent dir name for the output dir, if not set, \"output_opus\" will be used')

    args = parser.parse_args()

    src_dir = Path(args.music_dir).absolute()
    if args.parent_dir_name:
        dest_dir = src_dir / src_dir.parts[-1]
    else:
        dest_dir = src_dir / 'output_opus'
    dest_dir.mkdir(exist_ok=True)

    flac_files = list(src_dir.glob('*.flac'))

    for flac_file in flac_files:
        output_path = dest_dir / flac_file.name.replace('.flac', '.opus')
        cmd = ['ffmpeg', '-i', str(flac_file), '-c:v', 'libtheora', '-q:v', '10', '-c:a', 'libopus', '-b:a', '160k', str(output_path)]
        print(' '.join(cmd))
        subprocess.run(cmd, text=True)        

if __name__ == '__main__':
    main()
