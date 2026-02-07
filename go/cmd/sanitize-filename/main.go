package main

import (
	"fmt"
	"os"

	"github.com/akamensky/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

func main() {
	parser := argparse.NewParser("", "Script for sanitizing filenames")

	s := parser.String("f", "filename", &argparse.Options{Required: true, Help: "Filename to sanitize"})
	replaceSpaces := parser.Flag("r", "replace-spaces", &argparse.Options{Help: "Replace spaces with dots (\" \" -> \".\")."})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Printf("Error: %v", parser.Usage(err))
		os.Exit(1)
	}

	result := fileutils.SanitizeFilename(*s, *replaceSpaces)
	fmt.Println(result)
}
