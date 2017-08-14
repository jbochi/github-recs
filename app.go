package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

const (
	gitHubAuthenticatedUserURL = "https://api.github.com/user"
	gitHubStarredURL           = "https://api.github.com/user/starred"
	gitHubAccessTokenURL       = "https://github.com/login/oauth/access_token"
)

var (
	gitHubClientID     = os.Getenv("GITHUB_CLIENT_ID")
	gitHubClientSecret = os.Getenv("GITHUB_CLIENT_SECRET")
	tpl                = map[string]*template.Template{
		"home": template.Must(template.ParseFiles("templates/base.html", "templates/home.html")),
		"recs": template.Must(template.ParseFiles("templates/base.html", "templates/recommendations.html")),
	}
	model *Model
)

type (
	homeTemplateVars struct {
		ClientID string
		Err      string
	}

	recommendationsTemplateVars struct {
		User  string
		Stars []string
		Recs  []RepositoryScore
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

func init() {
	var err error
	model, err = ReadModel("./data/")

	if err != nil {
		panic(fmt.Sprintf("Failed to create vector model %s", err))
	}

	http.HandleFunc("/", home)
	http.HandleFunc("/callback", callback)
}

func gitHubAuthenticatedRequest(r *http.Request, url string, result interface{}) error {
	cookie, _ := r.Cookie("token")
	if cookie == nil {
		return fmt.Errorf("Unauthorized")
	}
	ctx := appengine.NewContext(r)
	client := urlfetch.Client(ctx)
	gitHubToken := cookie.Value

	fullURL := url + "?access_token=" + gitHubToken
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)

	if err != nil {
		return err
	}

	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		return err
	}

	return nil
}

func authenticatedUser(r *http.Request) (string, error) {
	var result gitHubUserResponse
	err := gitHubAuthenticatedRequest(r, gitHubAuthenticatedUserURL, &result)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("Error from GitHub: %s", result.Error)
	}

	return result.User, nil
}

func starred(r *http.Request) (stars []string, err error) {
	var result []gitHubStarredResponse
	err = gitHubAuthenticatedRequest(r, gitHubStarredURL, &result)
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
	ctx := appengine.NewContext(r)

	user, err := authenticatedUser(r)
	if err == nil {
		stars, err = starred(r)
	}

	if err != nil {
		vars := homeTemplateVars{ClientID: gitHubClientID, Err: err.Error()}
		if vars.Err == "Unauthorized" {
			vars.Err = ""
		}
		if err = tpl["home"].ExecuteTemplate(w, "base.html", vars); err != nil {
			log.Errorf(ctx, "%v", err)
			http.Error(w, "template execution failed", http.StatusInternalServerError)
		}
		return
	}

	vars := recommendationsTemplateVars{}
	vars.User = user
	vars.Stars = stars

	if model == nil {
		http.Error(w, "model was not initialized", http.StatusInternalServerError)
		return
	}

	recs, err := model.Recommend(stars, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed: %v", err), http.StatusInternalServerError)
		return
	}
	vars.Recs = recs

	if err := tpl["recs"].ExecuteTemplate(w, "base.html", vars); err != nil {
		log.Errorf(ctx, "%v", err)
		http.Error(w, "template execution failed", http.StatusInternalServerError)
	}
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
	http.Redirect(w, r, "/", http.StatusFound)
}
