// main.go
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

//go:embed index.txt
//go:embed *.txt.gz
var bibleFS embed.FS

// Translations holds all user‐facing messages from the index frontmatter.
type Translations struct {
	LanguageCode          string `yaml:"language"`
	InvalidParamMessage   string `yaml:"invalidParamMessage"`
	AcceptedValuesMessage string `yaml:"acceptedValuesMessage"`
	NoVersesError         string `yaml:"noVersesError"`
}

var trans Translations

// BookInfo holds metadata for each embedded book.
type BookInfo struct {
	ID        int
	Name      string
	LineCount int
	File      string
}

var (
	books      []BookInfo
	rng        *rand.Rand
	categories []struct {
		Key           string
		LowID, HighID int
	}
	catMap   map[string]struct{ LowID, HighID int }
	catLabel map[string]string
)

func init() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	raw, err := bibleFS.ReadFile("index.txt")
	if err != nil {
		log.Fatalf("cannot read index.txt: %v", err)
	}

	// parse YAML frontmatter
	var content []byte
	if bytes.HasPrefix(raw, []byte("---\n")) {
		parts := bytes.SplitN(raw, []byte("\n---\n"), 2)
		if err := yaml.Unmarshal(parts[0], &trans); err != nil {
			log.Fatalf("failed to parse translations: %v", err)
		}
		content = parts[1]
	} else {
		content = raw
	}

	// build books[]
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) != 3 {
			continue
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		cnt, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		books = append(books, BookInfo{
			ID:        id,
			Name:      parts[1],
			LineCount: cnt,
			File:      fmt.Sprintf("%d.txt.gz", id),
		})
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("error reading index content: %v", err)
	}

	// named categories
	categories = []struct {
		Key           string
		LowID, HighID int
	}{
		{"ot", 10, 460},
		{"nt", 470, 730},
		{"pentateuch", 10, 50},
		{"historical", 60, 190},
		{"poetic", 220, 260},
		{"major", 290, 340},
		{"minor", 350, 460},
		{"gospels", 470, 500},
		{"apostolic", 510, 720},
		{"acts", 510, 510},
		{"paul", 520, 650},
		{"general", 660, 720},
		{"revelation", 730, 730},
	}

	catMap = make(map[string]struct{ LowID, HighID int }, len(categories))
	catLabel = make(map[string]string, len(categories))
	for _, c := range categories {
		catMap[c.Key] = struct{ LowID, HighID int }{c.LowID, c.HighID}
		slice := sliceRange(c.LowID, c.HighID)
		if len(slice) == 0 {
			catLabel[c.Key] = ""
		} else if c.LowID == c.HighID {
			catLabel[c.Key] = slice[0].Name
		} else {
			first := slice[0].Name
			last := slice[len(slice)-1].Name
			catLabel[c.Key] = first + " — " + last
		}
	}
}

// sliceRange returns books whose IDs ∈ [lowID…highID].
func sliceRange(lowID, highID int) []BookInfo {
	start, end := -1, -1
	for i, b := range books {
		if start < 0 && b.ID >= lowID {
			start = i
		}
		if b.ID <= highID {
			end = i
		}
		if b.ID > highID {
			break
		}
	}
	if start >= 0 && end >= start {
		return books[start : end+1]
	}
	return nil
}

func main() {
	// CLI flags
	narrow := flag.String("narrow", "", "category to narrow (e.g. ot, nt, gospels)")
	colorize := flag.Bool("c", false, "highlight numbers in soft green")
	flag.Parse()

	// prepare ANSI color codes
	prefix, suffix := "", ""
	if *colorize {
		prefix = "\033[32m" // green
		suffix = "\033[0m"  // reset
	}

	// validate narrow
	if *narrow != "" {
		if _, ok := catMap[*narrow]; !ok {
			fmt.Fprintf(os.Stderr, trans.InvalidParamMessage+"\n", *narrow)
			fmt.Fprintln(os.Stderr, trans.AcceptedValuesMessage)
			for _, c := range categories {
				fmt.Fprintf(os.Stderr, "  %s — %s\n", c.Key, catLabel[c.Key])
			}
			os.Exit(1)
		}
	}

	// build selection pool
	pool := books
	if *narrow != "" {
		r := catMap[*narrow]
		pool = sliceRange(r.LowID, r.HighID)
	}

	// total lines
	total := 0
	for _, b := range pool {
		total += b.LineCount
	}
	if total == 0 {
		fmt.Fprintln(os.Stderr, trans.NoVersesError)
		os.Exit(1)
	}

	// pick a random global line
	choice := rng.Intn(total) + 1

	// locate book + offset
	cum := 0
	var sel BookInfo
	var offset int
	for _, b := range pool {
		if choice <= cum+b.LineCount {
			sel = b
			offset = choice - cum
			break
		}
		cum += b.LineCount
	}

	// open & decompress
	f, err := bibleFS.Open(sel.File)
	if err != nil {
		fmt.Fprintln(os.Stderr, trans.NoVersesError)
		os.Exit(1)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, trans.NoVersesError)
		os.Exit(1)
	}
	defer gz.Close()

	// skip to chosen verse
	scanner := bufio.NewScanner(gz)
	for i := 1; i < offset; i++ {
		if !scanner.Scan() {
			break
		}
	}

	// header with highlighted numbers
	fmt.Printf("%s (line %s%d%s/%s%d%s)\n\n",
		sel.Name,
		prefix, offset, suffix,
		prefix, sel.LineCount, suffix,
	)

	// verses with highlighted verse numbers
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			fmt.Printf("%s%s%s %s\n",
				prefix, parts[0], suffix,
				parts[1],
			)
		} else {
			fmt.Println(line)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "error reading verses:", err)
		os.Exit(1)
	}
}
