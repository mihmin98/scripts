package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	// Parse command-line arguments
	musicDir := flag.String("music_dir", "", "Directory which contains the flac files")
	useParentDirName := flag.Bool("parent-dir-name", false, "Use the parent dir name for the output dir, if not set, 'output_opus' will be used")
	flag.Parse()

	if *musicDir == "" {
		fmt.Println("Error: music_dir is required.")
		flag.Usage()
		os.Exit(1)
	}

	srcDir := filepath.Clean(*musicDir)
	destDir := ""

	// Determine the destination directory
	if *useParentDirName {
		destDir = filepath.Join(srcDir, filepath.Base(srcDir))
	} else {
		destDir = filepath.Join(srcDir, "output_opus")
	}

	// Create the destination directory if it doesn't exist
	err := os.MkdirAll(destDir, os.ModePerm)
	if err != nil {
		fmt.Printf("Error creating destination directory: %v\n", err)
		os.Exit(1)
	}

	// Find all FLAC files in the source directory
	flacFiles, err := filepath.Glob(filepath.Join(srcDir, "*.flac"))
	if err != nil {
		fmt.Printf("Error finding FLAC files: %v\n", err)
		os.Exit(1)
	}

	if len(flacFiles) == 0 {
		fmt.Println("No FLAC files found in the source directory.")
		return
	}

	// Process each FLAC file
	for _, flacFile := range flacFiles {
		outputPath := filepath.Join(destDir, strings.Replace(filepath.Base(flacFile), ".flac", ".opus", 1))
		cmd := []string{"ffmpeg", "-i", flacFile, "-c:v", "libtheora", "-q:v", "10", "-c:a", "libopus", "-b:a", "160k", outputPath}

		fmt.Println(strings.Join(cmd, " "))
		err := exec.Command(cmd[0], cmd[1:]...).Run()
		if err != nil {
			fmt.Printf("Error processing file %s: %v\n", flacFile, err)
		}
	}
}
