package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/akamensky/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	name               *string
	year               *int
	metadataProvider   *string
	metadataProviderId *string
	replaceSpaces      *bool
}

var metadataProviderMap = map[string]string{
	"tmdb": "tmdbid",
	"tvdb": "tvdbid",
	"omdb": "imdbid",
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", "Script for generating a movie/show directory name for Jellyfin")

	args.name = parser.String("n", "name", &argparse.Options{Required: true, Help: "Name of the movie/show"})
	args.year = parser.Int("y", "year", &argparse.Options{Help: "Year of the movie/show"})
	args.metadataProvider = parser.Selector("m", "metadata-provider", slices.Collect(maps.Keys(metadataProviderMap)), &argparse.Options{Help: "Metadata provider for identification"})
	args.metadataProviderId = parser.String("i", "id", &argparse.Options{Help: "Metadata Provider Id for movie/show"})
	args.replaceSpaces = parser.Flag("r", "replace-spaces", &argparse.Options{Help: "Replace spaces with dots (\" \" -> \".\")."})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Printf("Error: %v", parser.Usage(err))
		os.Exit(1)
	}

	sb := strings.Builder{}
	sanitizedName := fileutils.SanitizeFilename(*args.name, *args.replaceSpaces)

	sb.WriteString(sanitizedName)
	if *args.year != 0 {
		yearStr := fmt.Sprintf(" (%v)", *args.year)
		sb.WriteString(yearStr)
	}
	if *args.metadataProvider != "" && *args.metadataProviderId != "" {
		metadataStr := fmt.Sprintf(" [%v-%v]", metadataProviderMap[*args.metadataProvider], *args.metadataProviderId)
		sb.WriteString(metadataStr)
	}

	fmt.Println(sb.String())
}

/*
TODO:
sa incerc sa fac un mod in care pot sa ii dau doar imdb id

*/
