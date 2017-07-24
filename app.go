package server

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jbochi/facts/vectormodel"
	"github.com/kshedden/gonpy"
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

	http.HandleFunc("/", handler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	if model == nil {
		fmt.Fprint(w, "model was not initialized")
		return
	}

	repositories := []string{"tensorflow/tensorflow"}
	recs, err := model.Recommend(repositories, 10)
	if err != nil {
		fmt.Fprintf(w, "Failed: %v", err)
		return
	}

	fmt.Fprintf(w, "GitHub Recs: %v", recs)
}
