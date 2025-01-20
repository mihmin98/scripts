from pathlib import Path
import shutil
import os
import argparse

lang_to_code = {
    'english': 'en',
    'romanian': 'ro'
}

code_to_lang = {
    'en': 'english',
    'ro': 'romanian'
}

def copy_sub(video_path: Path, verbose: bool):
    video_name = video_path.stem
    subs_dir = video_path.parent / 'Subs'
    if not subs_dir.exists():
        print(f'ERROR: {subs_dir} does not exist')
        return
    if (subs_dir / video_name).exists():
        subs_dir = subs_dir / video_name
    
    available_subs = list(subs_dir.glob("*.srt"))
    for lang in lang_to_code.keys():
        lang_subs = [sub for sub in available_subs if lang in sub.stem.lower()]
        lang_subs = sorted(lang_subs, key=lambda sub: sub.stat().st_size, reverse=True)
        if len(lang_subs) > 0:
            selected_sub = lang_subs[0]
            sub_dest_filename = f"{video_name}.{lang_to_code[lang]}.srt"
            sub_dest_path = video_path.parent / sub_dest_filename
            
            if verbose:
                print(f"{selected_sub} -> {sub_dest_path}")
            shutil.copy(selected_sub, sub_dest_path)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('video_dir', help='Directory which contains the video(s)')
    parser.add_argument('-v', '--verbose', help='verbose', action='store_true')

    args = parser.parse_args()

    verbose = args.verbose
    src_dir = Path(args.video_dir).absolute()
    video_files = list(src_dir.glob("*.mp4"))

    for video_file in video_files:
        copy_sub(video_file, verbose=verbose)


if __name__ == '__main__':
    main()