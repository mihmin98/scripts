package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/akamensky/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	videoDir *string
	verbose  *bool
}

var langToCode = map[string]string{
	"english":  "en",
	"romanian": "ro",
}

const (
	videoExtension    = ".mp4"
	subtitleExtension = ".srt"
	subsDirName       = "Subs"
)

func copySub(dirPath string, videoPath string, verbose bool) {
	videoName := getFileName(videoPath)
	subsDirPath := filepath.Join(dirPath, subsDirName)
	if _, err := os.Stat(subsDirPath); err != nil {
		log.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(subsDirPath, videoName)); err == nil {
		subsDirPath = filepath.Join(subsDirPath, videoName)
	}

	availableSubs := fileutils.GetFilesWithExtension(subsDirPath, subtitleExtension)
	for lang := range langToCode {
		// There shouldn't be more than 3 srt files per language
		subsForCurrentLang := make([]string, 0, 3)
		for _, sub := range availableSubs {
			subFileName := getFileName(sub)
			if strings.Contains(strings.ToLower(subFileName), strings.ToLower(lang)) {
				subsForCurrentLang = append(subsForCurrentLang, sub)
			}
		}

		if len(subsForCurrentLang) > 0 {
			slices.SortStableFunc(subsForCurrentLang, subtitleComparator)
			selectedSub := subsForCurrentLang[0]
			subDestFilename := fmt.Sprintf("%v.%v%v", videoName, langToCode[lang], subtitleExtension)
			subDestPath := filepath.Join(dirPath, subDestFilename)

			if verbose {
				fmt.Printf("%v -> %v\n", selectedSub, subDestPath)
			}
			fileutils.CopyFile(selectedSub, subDestPath)
		}
	}
}

func subtitleComparator(a, b string) int {
	aStat, err := os.Stat(a)
	if err != nil {
		fmt.Printf("Error retrieving size for file %v", a)
		return 0
	}

	bStat, err := os.Stat(b)
	if err != nil {
		fmt.Printf("Error retrieving size for file %v", b)
		return 0
	}

	return int(bStat.Size() - aStat.Size())
}

func getFileName(file string) string {
	return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", "Script to copy subtitles from Subs dir from RARBG downloads")

	args.videoDir = parser.String("d", "video-dir", &argparse.Options{Required: true, Help: "Directory which contains the video(s)"})
	args.verbose = parser.Flag("v", "verbose", &argparse.Options{Help: "Enable verbose output"})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Printf("Error: %v", parser.Usage(err))
		os.Exit(1)
	}

	if _, err := os.Stat(*args.videoDir); err != nil {
		log.Fatal(err)
	}

	videoFiles := fileutils.GetFilesWithExtension(*args.videoDir, videoExtension)
	if *args.verbose {
		fmt.Printf("Found %v videos at %v\n", len(videoFiles), args.videoDir)
	}

	for _, videoFile := range videoFiles {
		copySub(*args.videoDir, videoFile, *args.verbose)
	}
}
