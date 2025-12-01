// main.go
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"flag"
	"fmt"
	"html"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

//go:embed randfromkjv/index.txt
//go:embed randfromkjv/*.txt.gz
var bibleFS embed.FS

// Translations holds all user‐facing strings loaded from index.txt frontmatter.
type Translations struct {
	LanguageCode          string `yaml:"language"`
	InvalidParamTitle     string `yaml:"invalidParamTitle"`
	InvalidParamMessage   string `yaml:"invalidParamMessage"`
	AcceptedValuesMessage string `yaml:"acceptedValuesMessage"`
	NoVersesError         string `yaml:"noVersesError"`
	BookNotFoundError     string `yaml:"bookNotFoundError"`
	DecompressionError    string `yaml:"decompressionError"`
	VersePageTitleFormat  string `yaml:"versePageTitleFormat"`
}

var (
	trans      Translations
	books      []BookInfo
	rng        *rand.Rand
	categories []struct {
		Key           string
		LowID, HighID int
	}
	catMap   map[string]struct{ LowID, HighID int }
	catLabel map[string]string
)

type BookInfo struct {
	ID        int
	Name      string
	LineCount int
	File      string
}

func init() {
	// seed RNG
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	// read index.txt (with YAML frontmatter)
	raw, err := bibleFS.ReadFile("randfromkjv/index.txt")
	if err != nil {
		log.Fatalf("cannot read index.txt: %v", err)
	}

	// split YAML frontmatter from content
	var content []byte
	if bytes.HasPrefix(raw, []byte("---\n")) {
		parts := bytes.SplitN(raw, []byte("\n---\n"), 2)
		if err := yaml.Unmarshal(parts[0], &trans); err != nil {
			log.Fatalf("failed to parse translations: %v", err)
		}
		content = parts[1]
	} else {
		log.Println("warning: no frontmatter found, using defaults")
		content = raw
	}

	// scan book index lines
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

	// define named ranges
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

	// build lookup maps
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

// sliceRange returns books whose ID ∈ [lowID…highID].
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

func randomHandler(w http.ResponseWriter, r *http.Request) {
	narrow := r.URL.Query().Get("narrow")
	if narrow != "" {
		if _, ok := catMap[narrow]; !ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="%s">
<head>
  <meta charset="UTF-8">
  <title>%s</title>
  <style>
    body { font-family: sans-serif; background: #fff8f0; color: #333; padding: 2rem; }
    h1 { color: #c0392b; }
    ul { margin-top: 1em; }
    li { margin: 0.5em 0; }
    code { background: #eee; padding: 0.2em 0.4em; }
  </style>
</head>
<body>
  <h1>`+trans.InvalidParamMessage+`</h1>
  <p>`+trans.AcceptedValuesMessage+`</p>
  <ul>`, trans.LanguageCode, trans.InvalidParamTitle, html.EscapeString(narrow))

			for _, c := range categories {
				fmt.Fprintf(w,
					`<li><code>%s</code> — %s</li>`+"\n",
					html.EscapeString(c.Key),
					html.EscapeString(catLabel[c.Key]),
				)
			}

			fmt.Fprint(w, `
  </ul>
</body>
</html>`)
			return
		}
	}

	pool := books
	if narrow != "" {
		r := catMap[narrow]
		pool = sliceRange(r.LowID, r.HighID)
	}

	total := 0
	for _, b := range pool {
		total += b.LineCount
	}
	if total == 0 {
		http.Error(w, trans.NoVersesError, http.StatusBadRequest)
		return
	}

	choice := rng.Intn(total) + 1

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

	f, err := bibleFS.Open("randfromkjv/" + sel.File)
	if err != nil {
		http.Error(w, trans.BookNotFoundError, http.StatusNotFound)
		return
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		http.Error(w, trans.DecompressionError, http.StatusInternalServerError)
		return
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	for i := 1; i < offset; i++ {
		if !scanner.Scan() {
			break
		}
	}

	title := fmt.Sprintf(trans.VersePageTitleFormat, sel.Name, offset, sel.LineCount)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="%s">
<head>
  <meta charset="UTF-8">
  <title>%s</title>
  <style>
    body { background: #fafafa; color: #333; font-family: sans-serif; padding: 1rem; line-height: 1.6; }
    .verse-num { color: #4caf50; font-weight: bold; }
    .verses p { margin: 0.5em 0; }
  </style>
</head>
<body>
  <h1>%s</h1>
  <div class="verses">`, trans.LanguageCode, html.EscapeString(title), html.EscapeString(title))

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			fmt.Fprintf(w,
				`<p><span class="verse-num">%s</span> %s</p>`+"\n",
				html.EscapeString(parts[0]),
				parts[1],
			)
		} else {
			fmt.Fprintf(w, `<p>%s</p>`+"\n", html.EscapeString(line))
		}
	}

	fmt.Fprint(w, "</div></body></html>")
	if err := scanner.Err(); err != nil {
		log.Printf("scan error in %s: %v", sel.File, err)
	}
}

func main() {
	port := flag.Int("p", 1616, "port to listen on")
	flag.Parse()

	http.HandleFunc("/", randomHandler)
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("listening on http://localhost%s/?narrow=nt", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
