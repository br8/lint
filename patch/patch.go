package patch

import (
	"bufio"
	"log"
	"regexp"
	"strconv"
	"strings"
)

var rangeInfoLine, modifiedLine, notRemovedLine *regexp.Regexp

func init() {
	// To match lines like @@ -112,6 +112,7 @@ func main() {
	rangeInfoLine = regexp.MustCompile(`^@@ .+\+(\d+),`)
	// To match lines like + // Load NQuads and write them to internal storage.
	modifiedLine = regexp.MustCompile(`^\+\ `)
	// To match all others lines which are not removed(i.e. not changed and
	// blank lines.
	notRemovedLine = regexp.MustCompile(`^[^-]`)
}

func ChangedLines(patch string) map[int]int {
	var err error
	clMap := make(map[int]int)
	scanner := bufio.NewScanner(strings.NewReader(patch))
	// Position as required by Github API for commenting.
	// https://developer.github.com/v3/pulls/comments/#create-a-comment
	position := 0
	// line number in original file.
	lineNumber := 0
	for scanner.Scan() {
		text := scanner.Text()
		// TODO - Remove regex, write a simple iterator which checks this.
		if m := rangeInfoLine.FindStringSubmatch(text); m != nil {
			lineNumber, err = strconv.Atoi(m[1])
			if err != nil {
				log.Fatal(err)
			}
		} else if len(text) >= 2 && text[0] == '+' && text[1] != '+' {
			clMap[lineNumber] = position
			lineNumber++
		} else if m := notRemovedLine.FindStringSubmatch(text); m != nil || text == "" {
			lineNumber++
		}
		position++
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return clMap
}
