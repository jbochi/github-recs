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
