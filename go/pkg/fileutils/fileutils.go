package fileutils

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func GetFilesWithExtension(dirPath, extension string) []string {
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

func CopyFile(src, dest string) error {
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

func ReplaceExtension(filename, newExtension string) string {
	origExtension := filepath.Ext(filename)
	newFilename := strings.TrimSuffix(filename, origExtension) + newExtension
	return newFilename
}

func SanitizeFilename(filename string, replaceSpaces bool) string {
	// Replace invalid characters with dot
	// This regex matches characters that are typically invalid in filenames:
	// - Control characters (0x00-0x1F, 0x7F)
	// - Common special characters that cause issues on various OSes
	// - Forward slash and backslash (path separators)
	// - Colon (drive separator on Windows)
	// - Quotation marks, asterisks, question marks, etc.
	reg := regexp.MustCompile(`[\\/:*?"<>|]`)
	filename = reg.ReplaceAllString(filename, ".")

	// Also replace control characters
	filename = regexp.MustCompile(`[\x00-\x1F\x7F]`).ReplaceAllString(filename, ".")

	// Replace whitespace if requested
	if replaceSpaces {
		filename = regexp.MustCompile(`\s+`).ReplaceAllString(filename, ".")
	}

	// Trim leading/trailing dots and spaces
	filename = strings.Trim(filename, " .")

	// Replace multiple consecutive dots with single dot
	filename = regexp.MustCompile(`\.-`).ReplaceAllString(filename, ".")
	filename = regexp.MustCompile(`\.-`).ReplaceAllString(filename, ".")
	filename = regexp.MustCompile(`\.+`).ReplaceAllString(filename, ".")

	// Handle empty string case
	if filename == "" {
		return "unnamed"
	}

	return filename
}
