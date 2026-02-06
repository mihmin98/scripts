package fileutils

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
