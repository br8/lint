package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/golang/lint"
	"github.com/pawanrawal/dlint/patch"
)

var (
	accessToken = flag.String("token", "", "Access token for the Github API")
	basePath    = flag.String("repo", "https://api.github.com/repos/pawanrawal/ideal-octo-fortnight", "basePath for repo")
	debugMode   = flag.Bool("debug", true, "In debug mode comments are not published to Github")
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

func fetchFileInfo(prNum int) []ghFileInfo {
	fi := make([]ghFileInfo, 0, 2)
	resp, err := http.Get(fmt.Sprintf("%v/pulls/%d/files", *basePath, prNum))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&fi)
	if err != nil {
		log.Fatal(err)
	}
	return fi
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

func publishComments(prNum int, le []LintError) {
	// TODO - Check the same comment on the same line shouldn't be published twice.
	// TODO - Publish comments on only those lines that changed in this iteration.
	for _, e := range le {
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
		// fmt.Println("response Status:", resp.Status)
		// fmt.Println("response Headers:", resp.Header)
		// body, _ := ioutil.ReadAll(resp.Body)
		// fmt.Println("response Body:", string(body))
	}
}

// TODO - Implement.
func validateName(fileName string) bool {
	return true
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
	fi := fetchFileInfo(d.Number)
	// run them through golint to find errors.
	var le []LintError
	l := lint.Linter{}
	for _, file := range fi {
		if !validateName(file.Name) {
			continue
		}

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
			le = append(le, LintError{
				Path:     file.Name,
				Position: pos,
				Body:     p.Text,
				CommitId: sha,
			})
		}
	}
	// publish comments.
	publishComments(d.Number, le)
}

func main() {
	flag.Parse()
	http.HandleFunc("/payload", payloadHandler)
	err := http.ListenAndServe(":4567", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
	fmt.Println("HTTP server listening on port 4567")
}
