package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	musicDir         string
	useParentDirName bool
	bitrate          int
	verbose          bool
}

var args programArgs

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
	args = programArgs{}

	flag.StringVar(&args.musicDir, "music-dir", "", "Directory which contains the .flac files")
	flag.BoolVar(&args.useParentDirName, "parent-dir-name", false, "Use the parent dir name for the output dir, if not set, \"output_opus\" will be used. This is mandatory.")
	flag.IntVar(&args.bitrate, "bitrate", 160, "Bitrate in kbps for the output files. By default it is set to 160k")
	flag.BoolVar(&args.verbose, "v", false, "Enable verbose output")

	flag.Parse()

	if args.musicDir == "" {
		fmt.Println("Error: Music Dir is required")
		flag.Usage()
		os.Exit(1)
	}

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
