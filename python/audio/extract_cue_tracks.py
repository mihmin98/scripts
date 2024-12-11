from pathlib import Path
import os
import argparse
import re

FRAMES_PER_SECOND = 75

def convert_cue_time_to_seconds(cue_time: str) -> str:
    cue_min, cue_sec, cue_frames = cue_time.split(':', maxsplit=3)
    seconds = int(cue_min) * 60 + int(cue_sec)
    fraction = int(cue_frames) / FRAMES_PER_SECOND
    return f'{round(seconds + fraction, 6)}'

def convert_filename(filename: str) -> str:
    return re.sub(r'[^\w_.)( -]', '_', filename)

class CueSheet:
    def __init__(self):
        self.cue_sheet_path: Path = None
        self.filename: str = None
        self.performer: str = None
        self.title: str = None
        self.tracks: list[CueTrack] = []


    def parse_cue_sheet(self, cue_sheet: str | Path):
        if type(cue_sheet) is str:
            cue_sheet = Path(cue_sheet)
        self.cue_sheet_path = cue_sheet

        with open(cue_sheet, 'r', encoding='utf-8', errors='ignore') as f:
            lines = f.readlines()

        i = 0
        current_context = 'sheet'
        current_track: CueTrack = None
        while i < len(lines):
            line = lines[i].lstrip(" \t").rstrip(' \n')
            
            if current_context == 'sheet':
                if line.startswith("PERFORMER"):
                    start = line.find('"') + 1
                    end = line.find('"', start)
                    self.performer = line[start:end]

                elif line.startswith("TITLE"):
                    start = line.find('"') + 1
                    end = line.find('"', start)
                    self.title = line[start:end]

                elif line.startswith("FILE"):
                    start = line.find('"') + 1
                    end = line.find('"', start)
                    self.filename = line[start:end]

                elif line.startswith("TRACK"):
                    current_context = 'track'
                    current_track = CueTrack()
                    split_line = line.split()
                    current_track.track_index = int(split_line[1])

            elif current_context == 'track':
                if line.startswith("TITLE"):
                    start = line.find('"') + 1
                    end = line.find('"', start)
                    current_track.title = line[start:end]
                
                elif line.startswith("PERFORMER"):
                    start = line.find('"') + 1
                    end = line.find('"', start)
                    current_track.performer = line[start:end]

                elif line.startswith("INDEX 00"):
                    current_track.index_0 = line.removeprefix("INDEX 00").strip()
                    current_track.index_0_sec = convert_cue_time_to_seconds(current_track.index_0)

                elif line.startswith("INDEX 01"):
                    current_track.index_1 = line.removeprefix("INDEX 01").strip()
                    current_track.index_1_sec = convert_cue_time_to_seconds(current_track.index_1)
            
                elif line.startswith("TRACK"):
                    self.tracks.append(current_track)
                    current_track = CueTrack()
                    split_line = line.split()
                    current_track.track_index = int(split_line[1])

            i += 1

        self.tracks.append(current_track)
        self.compute_tracks_start_and_end()

    def compute_tracks_start_and_end(self):
        for i in range(len(self.tracks)):
            self.tracks[i].start = self.tracks[i].index_1_sec
            if i < len(self.tracks) - 1:
                # TODO: should i drop 1 frame?
                self.tracks[i].end = self.tracks[i + 1].index_1_sec

    def create_ffmpeg_commands(self, output_dir: str=None) -> list[str]:
        commands = []
        audio_dir = self.cue_sheet_path.parent
        src_audio_path = audio_dir / self.filename

        if output_dir is not None:
            output_dir_path = Path(output_dir)
            output_dir_path.mkdir(exist_ok=True)

        for track in self.tracks:
            output_filename = convert_filename(f'{track.track_index:02}. {track.title}.flac')
            
            if output_dir is not None:
                output_file_path = (output_dir_path / output_filename).absolute()
            else:
                output_file_path = (audio_dir / output_filename).absolute()
            
            command = ['ffmpeg', '-hide_banner', '-i', f'\"{src_audio_path}\"', '-map', '0:a', '-c:a', 'flac', '-map_metadata', '-1', '-map_chapters', '-1']
            
            command += ['-metadata', f'title=\"{track.title}\"', '-metadata', f'artist=\"{track.performer}\"', '-metadata', f'album=\"{self.title}\"']
            command += ['-metadata', f'track=\"{track.track_index:02}\"']
            
            command += ['-ss', track.start]
            if track.end is not None:
                command += ['-to', track.end]
            
            command += [f'\"{output_file_path}\"']

            commands.append(' '.join(command))

        return commands

class CueTrack:
    def __init__(self):
        self.track_index: int = None
        self.title: str = None
        self.performer: str = None
        self.index_1: str = None
        self.index_1_sec: str = None
        self.index_0: str = None
        self.index_0_sec: str = None
        self.start: str = None
        self.end: str = None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('cue_sheet', type=str, help='CUE sheet that will be used to extract audio tracks')
    parser.add_argument('--output-dir', type=str, help='Destination for the flac files. If not set, then the flac files will be created in the same dir as the cue sheet')

    args = parser.parse_args()

    sheet = CueSheet()
    sheet.parse_cue_sheet(args.cue_sheet)
    commands = sheet.create_ffmpeg_commands(output_dir=args.output_dir)
    
    for command in commands:
        print(command)
        os.system(command)

if __name__ == '__main__':
    main()
