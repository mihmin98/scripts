package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type programArgs struct {
	videoDir string
	verbose  bool
}

var args programArgs

var langToCode = map[string]string{
	"english":  "en",
	"romanian": "ro",
}

// var codeToLang = map[string]string{
// 	"en": "english",
// 	"ro": "romanian",
// }

const (
	videoExtension    = ".mp4"
	subtitleExtension = ".srt"
	subsDirName       = "Subs"
)

func getFilesWithExtension(dirPath string, extension string) []string {
	var files []string
	filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(d.Name()) == extension {
			files = append(files, path)
		}

		return nil
	})

	return files
}

func copySub(dirPath string, videoPath string) {
	videoName := getFileName(videoPath)
	subsDirPath := filepath.Join(dirPath, subsDirName)
	if _, err := os.Stat(subsDirPath); err != nil {
		log.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(subsDirPath, videoName)); err == nil {
		subsDirPath = filepath.Join(subsDirPath, videoName)
	}

	availableSubs := getFilesWithExtension(subsDirPath, subtitleExtension)
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

			if args.verbose {
				fmt.Printf("%v -> %v\n", selectedSub, subDestPath)
			}
			copyFile(selectedSub, subDestPath)
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

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func main() {
	args = programArgs{}

	flag.StringVar(&args.videoDir, "video-dir", "", "Directory which contains the video(s)")
	flag.BoolVar(&args.verbose, "v", false, "Enable verbose output")

	flag.Parse()

	if args.videoDir == "" {
		fmt.Println("Error: Video Dir is required")
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(args.videoDir); err != nil {
		log.Fatal(err)
	}

	videoFiles := getFilesWithExtension(args.videoDir, videoExtension)
	if args.verbose {
		fmt.Printf("Found %v videos at %v\n", len(videoFiles), args.videoDir)
	}

	for _, videoFile := range videoFiles {
		copySub(args.videoDir, videoFile)
	}
}
