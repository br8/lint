package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dgraph-io/lint/patch"
	"github.com/golang/lint"
)

var (
	port        = flag.String("port", ":4567", "Port to run the web server on.")
	accessToken = flag.String("token", "", "Access token for the Github API")
	basePath    = flag.String("repo", "", "basePath for repo")
	ignoreFile  = flag.String("ignore", "", "Name of file which contains information about files/folders which should be ignored")
	debugMode   = flag.Bool("debug", false, "In debug mode comments are not published to Github")
)

func url(path string) string {
	return ""
}

type ghFileInfo struct {
	Name   string `json:"filename"`
	Status string
	RawUrl string `json:"raw_url"`
	Patch  string
}

type commitInfo struct {
	Sha   string       `json:"sha"`
	Files []ghFileInfo `json:"files"`
}

func fetchFileInfo(sha string) []ghFileInfo {
	ci := commitInfo{}
	resp, err := http.Get(fmt.Sprintf("%v/commits/%s", *basePath, sha))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&ci)
	if err != nil {
		log.Fatal(err)
	}
	return ci.Files
}

func fetchFileContent(rawUrl string) []byte {
	resp, err := http.Get(rawUrl)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	return b
}

// LintError also doubles up as body for creating a comment.
// https://developer.github.com/v3/pulls/comments/#create-a-comment
type LintError struct {
	Path     string `json:"path"`
	Position int    `json:"position"`
	Body     string `json:"body"`
	CommitId string `json:"commit_id"`
}

func publishComments(prNum int, pc chan LintError) {
	// TODO - Check the same comment on the same line shouldn't be published twice.
	for e := range pc {
		if *debugMode {
			fmt.Printf("File: %v, error: %v at position %v.\n", e.Path, e.Body, e.Position)
			continue
		}
		url := fmt.Sprintf("%v/pulls/%d/comments", *basePath, prNum)
		b, err := json.Marshal(&e)
		if err != nil {
			log.Fatal(err)
		}
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
		req.Header.Set("Authorization", fmt.Sprintf("token %v", *accessToken))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
	}
}

func lintFile(fileName string) bool {
	if ext := filepath.Ext(fileName); ext != ".go" {
		return false
	}
	for _, ig := range ignored {
		if ig.itype == FILE && ig.name == fileName {
			return false
		} else if ig.itype == EXT && strings.HasSuffix(fileName, ig.name) {
			return false
		} else if ig.itype == DIR && strings.HasPrefix(fileName, ig.name) {
			return false
		}
	}
	return true
}

func findErrors(file ghFileInfo, sha string, pc chan LintError, wg *sync.WaitGroup) {
	defer wg.Done()
	l := lint.Linter{}
	c := fetchFileContent(file.RawUrl)
	// TODO - Add support for multiple linters like govet, errcheck.
	problems, err := l.Lint(file.Name, c)
	if err != nil {
		log.Fatal(err)
	}
	cl := patch.ChangedLines(file.Patch)
	var pos int
	var ok bool
	for _, p := range problems {
		if p.Confidence < 0.8 {
			continue
		}
		// Check the problem line should be part of changed lines in this PR.
		if pos, ok = cl[p.Position.Line]; !ok {
			continue
		}
		pc <- LintError{
			Path:     file.Name,
			Position: pos,
			Body:     p.Text,
			CommitId: sha,
		}
	}
}

// handler for incoming webhook that is triggered when a PR is created/modified.
func payloadHandler(w http.ResponseWriter, r *http.Request) {
	// parse pull request number.
	decoder := json.NewDecoder(r.Body)

	type pr struct {
		Head struct {
			Sha string
		}
	}

	type data struct {
		Number      int
		PullRequest pr `json:"pull_request"`
	}

	var d data
	err := decoder.Decode(&d)
	if err != nil {
		log.Fatal(err)
	}
	sha := d.PullRequest.Head.Sha

	// get files in PR.
	fi := fetchFileInfo(sha)
	problemsCh := make(chan LintError, 100)

	go publishComments(d.Number, problemsCh)
	var wg sync.WaitGroup
	for _, file := range fi {
		if !lintFile(file.Name) {
			continue
		}
		if *debugMode {
			fmt.Printf("File info %+v\n", file)
		}
		wg.Add(1)
		go findErrors(file, sha, problemsCh, &wg)
	}
	wg.Wait()
	close(problemsCh)
}

const (
	FILE = iota
	DIR
	EXT
)

type ignore struct {
	name  string
	itype int
}

var ignored []ignore

func parseFilesToIgnore() {
	if *ignoreFile == "" {
		return
	}
	f, err := os.Open(*ignoreFile)
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l := scanner.Text()
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		ig := ignore{name: l}
		ext := filepath.Ext(l)
		if l[0] == '*' && len(l) > 1 {
			ig.name = l[1:]
			ig.itype = EXT
		} else if ext != ".go" {
			ig.itype = DIR
		}
		ignored = append(ignored, ig)
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()
	if *basePath == "" {
		log.Fatal("Please enter a valid base path for a Github repo.")
	}
	parseFilesToIgnore()
	http.HandleFunc("/payload", payloadHandler)
	fmt.Println("HTTP server listening on port 4567")
	err := http.ListenAndServe(*port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
