import argparse
import subprocess
from pathlib import Path

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('go_file', help='go program source file that should be compiled')

    args = parser.parse_args()

    go_file_path = Path(args.go_file)
    output_dir = go_file_path.parent / 'build'
    output_dir.mkdir(exist_ok=True)

    linux_exec_path = output_dir / f'{go_file_path.stem}-x64'
    win_exec_path = output_dir / f'{go_file_path.stem}-x64.exe'

    linux_cmd = ['go', 'build', '-o', linux_exec_path.name, str(go_file_path)]
    linux_env = {'GOOS': 'linux', 'GOOARCH': 'amd64'}
    print(' '.join(linux_cmd))
    subprocess.run(linux_cmd, env=linux_env)

    win_cmd = ['go', 'build', '-o', win_exec_path.name, str(go_file_path)]
    win_env = {'GOOS': 'windows', 'GOOARCH': 'amd64'}
    subprocess.run(win_cmd, env=win_env)

if __name__ == '__main__':
    main()
