package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	musicDir         string
	useParentDirName bool
	bitrate          int
	verbose          bool
}

const (
	flacExtension        = ".flac"
	opusExtension        = ".opus"
	defaultOutputDirName = "output_opus"
)

func createOutputDir(musicDir string, useParentDirName bool) (string, error) {
	var outputDir string
	if useParentDirName {
		outputDir = filepath.Join(musicDir, filepath.Base(musicDir))
	} else {
		outputDir = filepath.Join(musicDir, defaultOutputDirName)
	}

	err := os.MkdirAll(outputDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	return outputDir, nil
}

func convertFlac(flacPath, outputDir string, bitrate int) error {
	origFilename := filepath.Base(flacPath)
	outputFilename := fileutils.ReplaceExtension(origFilename, opusExtension)

	outputFile := filepath.Join(outputDir, outputFilename)

	outputBitrate := fmt.Sprintf("%vk", bitrate)

	cmd := exec.Command("ffmpeg", "-i", flacPath, "-c:v", "libtheora", "-q:v", "10", "-c:a", "libopus", "-b:a", outputBitrate, outputFile)
	err := cmd.Run()
	return err
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", "Script for converting flac files to opus files")

	parser.MustAddArgument([]string{"-d", "--music-dir"}, &argparse.Options{Required: true, Help: "Path to directory which contains the .flac files"})
	parser.MustAddArgument([]string{"-p", "--parent-dir-name"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Use the parent dir name for the output dir, if not set, \"output_opus\" will be used."})
	parser.MustAddArgument([]string{"-b", "--bitrate"}, &argparse.Options{Type: argparse.Int, Default: 160, Help: "Bitrate in kbps for the output files. By default it is set to 160k"})
	parser.MustAddArgument([]string{"-v", "--verbose"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Enable verbose output"})

	ns := parser.MustParseArgs(os.Args[1:])
	args.musicDir = ns.String("music_dir")
	args.useParentDirName = ns.Bool("parent_dir_name")
	args.bitrate = ns.Int("bitrate")
	args.verbose = ns.Bool("verbose")

	if _, err := os.Stat(args.musicDir); err != nil {
		log.Fatal(err)
	}

	musicFiles := fileutils.GetFilesWithExtension(args.musicDir, flacExtension)
	if args.verbose {
		fmt.Printf("Found %v flac files at %v\n", len(musicFiles), args.musicDir)
	}

	outputDir, err := createOutputDir(args.musicDir, args.useParentDirName)
	if err != nil {
		log.Fatal(err)
	}

	for _, musicFile := range musicFiles {
		if args.verbose {
			fmt.Printf("Converting \"%v\"...\n", filepath.Base(musicFile))
		}
		err = convertFlac(musicFile, outputDir, args.bitrate)
		if err != nil {
			fmt.Printf("Error converting \"%v\": %v\n", musicFile, err)
		}
	}
}

/*

TODO: - sa vad de ce nu imi face aia cu parent dir
- sa il fac sa ruleze in paralel

*/
