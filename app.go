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
	gitHubAccessTokenURL = "https://github.com/login/oauth/access_token"
	homeTemplate         = `<html>
	<head>
	</head>
	<body>
		<p>
			Well, hello there! To generate recommendations just for you, I need to get all the beautiful stars you gave.
		</p>
		<p>
			We're going to now talk to the GitHub API. Ready?
			<a href="https://github.com/login/oauth/authorize?scope=user:email&client_id={{.ClientID}}">Click here</a> to begin!</a>
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
	}

	gitHubAccessTokenResponse struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		ErrorURI         string `json:"error_uri"`
		AccessToken      string `json:"access_token"`
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
	http.HandleFunc("/recommendations", recommendations)

}

func home(w http.ResponseWriter, r *http.Request) {
	vars := homeTemplateVars{ClientID: gitHubClientID}
	t := template.Must(template.New("home").Parse(homeTemplate))
	t.Execute(w, vars)
}

func recommendations(w http.ResponseWriter, r *http.Request) {
	if model == nil {
		http.Error(w, "model was not initialized", http.StatusInternalServerError)
		return
	}

	repositories := []string{"tensorflow/tensorflow"}
	recs, err := model.Recommend(repositories, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "GitHub Recs: %v", recs)
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

	fmt.Fprintf(w, "Token: %v", result.AccessToken)
}
