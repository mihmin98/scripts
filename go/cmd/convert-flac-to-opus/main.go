package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/akamensky/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	musicDir         *string
	useParentDirName *bool
	bitrate          *int
	verbose          *bool
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

	cmd := "ffmpeg"
	cmdArgs := []string{"-i", flacPath, "-c:v", "libtheora", "-q:v", "10", "-c:a", "libopus", "-b:a", outputBitrate, outputFile}

	err := exec.Command(cmd, cmdArgs...).Run()
	return err
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", "Script for converting flac files to opus files")

	args.musicDir = parser.String("d", "music-dir", &argparse.Options{Required: true, Help: "Path to directory which contains the .flac files"})
	args.useParentDirName = parser.Flag("p", "parent-dir-name", &argparse.Options{Help: "Use the parent dir name for the output dir, if not set, \"output_opus\" will be used."})
	args.bitrate = parser.Int("b", "bitrate", &argparse.Options{Help: "Bitrate in kbps for the output files. By default it is set to 160k", Default: 160})
	args.verbose = parser.Flag("v", "verbose", &argparse.Options{Help: "Enable verbose output"})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Printf("Error: %v", parser.Usage(err))
		os.Exit(1)
	}

	if _, err := os.Stat(*args.musicDir); err != nil {
		log.Fatal(err)
	}

	musicFiles := fileutils.GetFilesWithExtension(*args.musicDir, flacExtension)
	if *args.verbose {
		fmt.Printf("Found %v flac files at %v\n", len(musicFiles), args.musicDir)
	}

	outputDir, err := createOutputDir(*args.musicDir, *args.useParentDirName)
	if err != nil {
		log.Fatal(err)
	}

	for _, musicFile := range musicFiles {
		if *args.verbose {
			fmt.Printf("Converting \"%v\"...\n", filepath.Base(musicFile))
		}
		err = convertFlac(musicFile, outputDir, *args.bitrate)
		if err != nil {
			fmt.Printf("Error converting \"%v\": %v\n", musicFile, err)
		}
	}
}
