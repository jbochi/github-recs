package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jbochi/facts/vectormodel"
	"github.com/kshedden/gonpy"
	"google.golang.org/appengine"
	"google.golang.org/appengine/urlfetch"
)

var (
	gitHubClientID     = os.Getenv("GITHUB_CLIENT_ID")
	gitHubClientSecret = os.Getenv("GITHUB_CLIENT_SECRET")
)

const (
	gitHubAuthenticatedUserURL = "https://api.github.com/user"
	gitHubStarredURL           = "https://api.github.com/user/starred"
	gitHubAccessTokenURL       = "https://github.com/login/oauth/access_token"
	homeTemplate               = `<html>
	<head>
	</head>
	<body>
		<h1>GitHub Repository Recommender</h1>
		<p>
			Well, hello there! To generate recommendations just for you, I need to get all the beautiful stars you gave.
		</p>
		{{ if .Err }}
		<p>
			I tried to get them, but something went wrong: <b>{{.Err}}</b>
		</p>
		{{ end }}
		<p>
			We're going to now talk to the GitHub API. Ready?
			<b><a href="https://github.com/login/oauth/authorize?scope=&client_id={{.ClientID}}">Click here</a></b> to begin!</a>
		</p>
	</body>
	</html>`
)

type (
	// Model is the struct that handles recommendations
	Model struct {
		vm            *vectormodel.VectorModel
		repositories  []string
		repositoryIDs map[string]int
	}

	// RepositoryScore is a pair of repo / score
	RepositoryScore struct {
		repository string
		score      float64
	}

	homeTemplateVars struct {
		ClientID string
		Err      string
	}

	gitHubAccessTokenResponse struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		ErrorURI         string `json:"error_uri"`
		AccessToken      string `json:"access_token"`
		Scope            string `json:"scope"`
	}

	gitHubUserResponse struct {
		Error string `json:"error"`
		User  string `json:"login"`
	}

	gitHubStarredResponse struct {
		Repository string `json:"full_name"`
	}
)

var model *Model

// ReadModel returns a VectorModel from given file path
func ReadModel(path string) (*Model, error) {
	confidence := 3.0
	regularization := 0.001

	rdr, err := gonpy.NewFileReader(path + "item_factors.npy")
	if err != nil {
		return nil, fmt.Errorf("Unable to read data: %v", err)
	}
	nRepositories, nFactors := rdr.Shape[0], rdr.Shape[1]

	data, err := rdr.GetFloat64()
	if err != nil {
		return nil, fmt.Errorf("Unable to parse data: %v", err)
	}

	docs := make(map[int][]float64)
	for i := 0; i < nRepositories; i++ {
		docs[i] = data[i*nFactors : (i+1)*nFactors]
	}

	vm, err := vectormodel.NewVectorModel(docs, confidence, regularization)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path + "items.csv")
	if err != nil {
		return nil, fmt.Errorf("Unable to open items.csv: %v", err)
	}

	repositories := make([]string, nRepositories)
	repositoryIDs := map[string]int{}

	reader := bufio.NewReader(f)
	for i := 0; i < rdr.Shape[0]; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("Unable to read line of file: %v", err)
		}
		repo := strings.TrimRight(line, "\n")
		repositories[i] = repo
		repositoryIDs[repo] = i
	}

	m := &Model{
		vm:            vm,
		repositories:  repositories,
		repositoryIDs: repositoryIDs,
	}
	return m, nil
}

// Recommend returns a list of recommended repositories
func (m *Model) Recommend(items []string, n int) ([]RepositoryScore, error) {
	seenDocs := map[int]bool{}
	for _, repo := range items {
		repoID, ok := m.repositoryIDs[repo]
		if ok {
			seenDocs[repoID] = true
		}
	}
	scores, err := m.vm.Recommend(&seenDocs, n)
	if err != nil {
		return nil, err
	}
	results := []RepositoryScore{}
	for _, score := range scores {
		result := RepositoryScore{m.repositories[score.DocumentID], score.Score}
		results = append(results, result)
	}
	return results, nil
}

func init() {
	var err error
	model, err = ReadModel("./data/")

	if err != nil {
		panic(fmt.Sprintf("Failed to create vector model %s", err))
	}
	if model == nil {
		panic("Something went wrong")
	}

	http.HandleFunc("/", home)
	http.HandleFunc("/callback", callback)
}

func authenticatedUser(r *http.Request) (string, error) {
	cookie, _ := r.Cookie("token")
	if cookie == nil {
		return "", fmt.Errorf("Unauthorized")
	}
	ctx := appengine.NewContext(r)
	client := urlfetch.Client(ctx)
	gitHubToken := cookie.Value

	url := gitHubAuthenticatedUserURL + "?access_token=" + gitHubToken
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)

	if err != nil {
		return "", err
	}

	var result gitHubUserResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("Error from GitHub: %s", result.Error)
	}

	return result.User, nil
}

func starred(r *http.Request) (stars []string, err error) {
	cookie, _ := r.Cookie("token")
	if cookie == nil {
		return stars, fmt.Errorf("Unauthorized")
	}
	ctx := appengine.NewContext(r)
	client := urlfetch.Client(ctx)
	gitHubToken := cookie.Value

	url := gitHubStarredURL + "?access_token=" + gitHubToken
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return stars, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)

	if err != nil {
		return stars, err
	}

	var result []gitHubStarredResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return stars, err
	}

	for _, r := range result {
		stars = append(stars, r.Repository)
	}

	return stars, err
}

func home(w http.ResponseWriter, r *http.Request) {
	var stars []string
	user, err := authenticatedUser(r)
	if err == nil {
		stars, err = starred(r)
	}

	if err != nil {
		vars := homeTemplateVars{ClientID: gitHubClientID, Err: err.Error()}
		if vars.Err == "Unauthorized" {
			vars.Err = ""
		}
		t := template.Must(template.New("home").Parse(homeTemplate))
		t.Execute(w, vars)
		return
	}

	fmt.Fprint(w, "<html><body><h1>GitHub Repository Recommender</h1><p>Hey! I know you! <b>")
	fmt.Fprint(w, user)
	fmt.Fprint(w, "</b>, isn't it?</p>")

	if len(stars) == 0 {
		fmt.Fprint(w, "<p>Sorry, I can't recommend because you have not starred any repos.</p>")
		return
	}

	if model == nil {
		http.Error(w, "model was not initialized", http.StatusInternalServerError)
		return
	}

	recs, err := model.Recommend(stars, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, "<h2>GitHub Recs:</h2><ul>")
	for _, rec := range recs {
		fmt.Fprintf(w, "<li><a href=\"https://github.com/%s\">%s</a> (%f)</li>",
			rec.repository, rec.repository, rec.score)
	}
	fmt.Fprint(w, "</ul>")

	fmt.Fprint(w, "<h2>You starred:</h2><ul>")
	for _, star := range stars {
		fmt.Fprintf(w, "<li><a href=\"https://github.com/%s\">%s</a></li>", star, star)
	}
	fmt.Fprintf(w, "</ul>")

	fmt.Fprint(w, "</body></html>")
}

func callback(w http.ResponseWriter, r *http.Request) {
	// create request to get token
	sessionCode := r.FormValue("code")
	ctx := appengine.NewContext(r)
	client := urlfetch.Client(ctx)
	values := url.Values{
		"client_id":     []string{gitHubClientID},
		"client_secret": []string{gitHubClientSecret},
		"code":          []string{sessionCode},
	}
	body := values.Encode()

	req, err := http.NewRequest("POST", gitHubAccessTokenURL, strings.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// issue request
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		fmt.Fprintf(w, "Something went wrong! %v", err)
		return
	}

	// extract the token and granted scopes
	var result gitHubAccessTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if result.Error != "" {
		http.Error(w, result.Error, http.StatusInternalServerError)
		return
	}

	expiration := time.Now().Add(10 * time.Minute)
	cookie := http.Cookie{Name: "token", Value: result.AccessToken, Expires: expiration}
	http.SetCookie(w, &cookie)

	fmt.Fprintf(w, "Get your cookie: %v\n", result.AccessToken)
	fmt.Fprintf(w, "Thanks for: %v\n", result.Scope)
}
