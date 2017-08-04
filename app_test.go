package server

import (
	"testing"
)

func TestModel(t *testing.T) {
	model, err := ReadModel("./data/")
	if err != nil {
		t.Fatalf("Unable to read model: %v", err)
	}
	if model == nil {
		t.Fatalf("Did not return a model")
	}
	recs, err := model.Recommend([]string{"tensorflow/tensorflow", "BVLC/caffe"}, 10)
	if err != nil {
		t.Errorf("Failed to recommend: %s", err)
	}
	if len(recs) != 10 {
		t.Errorf("Wrong number of recommendations: %v", recs)
	}
}

func BenchmarkModel(b *testing.B) {
	model, err := ReadModel("./data/")
	if err != nil {
		b.Fatalf("Unable to read model: %v", err)
	}
	if model == nil {
		b.Fatalf("Did not return a model")
	}
	var recs []RepositoryScore

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recs, err = model.Recommend([]string{"tensorflow/tensorflow", "BVLC/caffe"}, 10)
	}

	if err != nil {
		b.Errorf("Failed to recommend: %s", err)
	}
	if len(recs) != 10 {
		b.Errorf("Wrong number of recommendations: %v", recs)
	}
}
