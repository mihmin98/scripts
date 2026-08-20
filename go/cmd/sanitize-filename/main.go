package main

import (
	"fmt"
	"os"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

func main() {
	parser := argparse.NewParser("", "Script for sanitizing filenames")

	parser.MustAddArgument([]string{"-f", "--filename"}, &argparse.Options{Required: true, Help: "Filename to sanitize"})
	parser.MustAddArgument([]string{"-r", "--replace-spaces"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Replace spaces with dots (\" \" -> \".\")."})

	ns := parser.MustParseArgs(os.Args[1:])

	result := fileutils.SanitizeFilename(ns.String("filename"), ns.Bool("replace_spaces"))
	fmt.Println(result)
}
