package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/lint/patch"
	"github.com/golang/lint"
)

var (
	port         = flag.String("port", ":4567", "Port to run the web server on.")
	basePath     = flag.String("repo", "", "basePath for repo")
	ignoreFile   = flag.String("ignore", "", "Name of file which contains information about files/folders which should be ignored")
	clientId     = flag.String("cid", "", "Client id used for Github application")
	clientSecret = flag.String("csecret", "", "Client secret used for Github application")
	serverAddr   = flag.String("ip", "", "Public IP address with port of the server")
	debugMode    = flag.Bool("debug", false, "In debug mode comments are not published to Github")
	accessToken  = ""
)

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
		req.Header.Set("Authorization", fmt.Sprintf("token %v", accessToken))
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
	if accessToken == "" {
		return
	}
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

func randString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

var state string

func requestAccessHandler(w http.ResponseWriter, r *http.Request) {
	if accessToken != "" {
		w.Write([]byte("Access already granted."))
		return
	}
	req, err := http.NewRequest("GET", "https://github.com/login/oauth/authorize", nil)
	if err != nil {
		fmt.Println(err)
		w.Write([]byte("Something went wrong."))
		return
	}
	q := req.URL.Query()
	q.Add("client_id", *clientId)
	q.Add("scope", "repo write:repo_hook")
	state = randString(15)
	q.Add("state", state)
	req.URL.RawQuery = q.Encode()
	fmt.Println(req.URL.String())
	http.Redirect(w, r, req.URL.String(), http.StatusFound)
}

type OAuthRes struct {
	AccessToken string `json:"access_token"`
}

type whBody struct {
	Name   string   `json:"name"`
	Config config   `json:"config"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
}

type config struct {
	Url         string `json:"url"`
	ContentType string `json:"content_type"`
}

func createWebhook() error {
	body := whBody{
		Name:   "web",
		Events: []string{"pull_request"},
		Active: true,
		Config: config{
			Url:         fmt.Sprintf("%v/payload", *serverAddr),
			ContentType: "json",
		},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	fmt.Println(string(buf))
	req, err := http.NewRequest("POST", fmt.Sprintf("%v/hooks", *basePath), bytes.NewBuffer(buf))
	fmt.Println(req.URL.String())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %v", accessToken))
	c := http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	fmt.Println(resp.StatusCode)
	if resp.StatusCode != 201 {
		return fmt.Errorf("Unexpected error code")
	}
	return nil
}

func callbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	s := r.URL.Query().Get("state")
	if code == "" || s != state {
		w.Write([]byte("Process couldn't be completed, please try again."))
		return
	}

	form := url.Values{}
	form.Add("client_id", *clientId)
	form.Add("client_secret", *clientSecret)
	form.Add("code", code)
	form.Add("state", state)
	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		fmt.Println(err)
		w.Write([]byte("Something went wrong."))
		return
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	c := http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		w.Write([]byte("Process couldn't be completed, please try again."))
		return
	}
	// TODO - Check scopes granted and respond with error if they don't match
	// requested scopes.
	var or OAuthRes
	err = json.NewDecoder(resp.Body).Decode(&or)
	if err != nil {
		w.Write([]byte("Process couldn't be completed, please try again."))
		return
	}
	if or.AccessToken == "" {
		w.Write([]byte("Process couldn't be completed, please try again."))
		return
	}
	accessToken = or.AccessToken
	fmt.Println(accessToken)
	// Create webhook which would be informed about pull request changes.
	if err = createWebhook(); err != nil {
		fmt.Println(err)
		w.Write([]byte("Process couldn't be completed, please try again."))
		return
	}
	w.Write([]byte("Access granted successfully"))
}

func main() {
	rand.Seed(time.Now().UnixNano())
	flag.Parse()
	if *basePath == "" {
		log.Fatal("Please enter a valid base path for a Github repo.")
	}
	if *clientId == "" || *clientSecret == "" {
		log.Fatal("Github app credentials missing")
	}
	if *serverAddr == "" {
		log.Fatal("Server IP address missing")
	}

	parseFilesToIgnore()
	http.HandleFunc("/", requestAccessHandler)
	http.HandleFunc("/callback", callbackHandler)
	http.HandleFunc("/payload", payloadHandler)
	fmt.Println("HTTP server listening on port 4567")
	err := http.ListenAndServe(*port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
