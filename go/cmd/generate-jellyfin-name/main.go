package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/mihmin98/scripts/pkg/fileutils"
)

type programArgs struct {
	name               string
	year               int
	metadataProvider   string
	metadataProviderId string
	replaceSpaces      bool
}

var metadataProviderMap = map[string]string{
	"tmdb": "tmdbid",
	"tvdb": "tvdbid",
	"omdb": "imdbid",
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", "Script for generating a movie/show directory name for Jellyfin")

	parser.MustAddArgument([]string{"-n", "--name"}, &argparse.Options{Required: true, Help: "Name of the movie/show"})
	parser.MustAddArgument([]string{"-y", "--year"}, &argparse.Options{Type: argparse.Int, Help: "Year of the movie/show"})
	parser.MustAddArgument([]string{"-m", "--metadata-provider"}, &argparse.Options{Choices: slices.Sorted(maps.Keys(metadataProviderMap)), Help: "Metadata provider for identification"})
	parser.MustAddArgument([]string{"--id"}, &argparse.Options{Help: "Metadata Provider Id for movie/show"})
	parser.MustAddArgument([]string{"-r", "--replace-spaces"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Replace spaces with dots (\" \" -> \".\")."})

	ns := parser.MustParseArgs(os.Args[1:])
	args.name = ns.String("name")
	args.year = ns.Int("year")
	args.metadataProvider = ns.String("metadata_provider")
	args.metadataProviderId = ns.String("id")
	args.replaceSpaces = ns.Bool("replace_spaces")

	sb := strings.Builder{}
	sanitizedName := fileutils.SanitizeFilename(args.name, args.replaceSpaces)

	sb.WriteString(sanitizedName)
	if args.year != 0 {
		yearStr := fmt.Sprintf(" (%v)", args.year)
		sb.WriteString(yearStr)
	}
	if args.metadataProvider != "" && args.metadataProviderId != "" {
		metadataStr := fmt.Sprintf(" [%v-%v]", metadataProviderMap[args.metadataProvider], args.metadataProviderId)
		sb.WriteString(metadataStr)
	}

	fmt.Println(sb.String())
}

/*
TODO:
sa incerc sa fac un mod in care pot sa ii dau doar imdb id si sa imi faca un fel de scrape la pagina de imdb din care sa ia titlul

vad ca exista o librarie numita goquery

cum sa structurez aplicatia atunci?
ii mai bag un param care sa fie --imdb: si e id-ul tt00000000
dupa ma duc la https://imdb.com/title/{imdb_id}

ma uit in head -> title si extrag titlul

Pentru filme vad ca e de tipul: "{Movie.Name} ({year}) - IMDb" (ex: <title>Star Wars: Episode III - Revenge of the Sith (2005) - IMDb</title>)
pentru seriale vad ca e (<title>The Sopranos (TV Series 1999–2007) - IMDb</title>)

deci in primul rand trebuie sa tai sufixul " - IMDb"
asta ma lasa cu paranteza de final cu anul
dupa trebuie sa caut ultima "(" si sa iau substring-ul din paranteze
il fac lower si vad daca contine tv series; daca da, atunci nu iau anul
dupa iau tot ce e inaintea parantezei "(" si ala e titlul pe care il sanitizez
EZ PZ
*/
