package service

import (
	"math"
	"testing"
)

func TestRocchioBasic(t *testing.T) {
	queryVec := []float32{1.0, 0.0, 0.0, 0.0}
	docVecs := [][]float32{
		{0.0, 1.0, 0.0, 0.0},
		{0.0, 0.0, 1.0, 0.0},
	}

	result := Rocchio(queryVec, docVecs, 0.6, 0.4)

	if len(result) != 4 {
		t.Fatalf("expected dim 4, got %d", len(result))
	}
	if result[0] < 0.8 || result[0] > 1.0 {
		t.Fatalf("expected query component preserved, got %f", result[0])
	}
}

func TestRocchioNoDocs(t *testing.T) {
	queryVec := []float32{1.0, 0.0, 0.0, 0.0}
	result := Rocchio(queryVec, nil, 0.6, 0.4)

	if len(result) != 4 {
		t.Fatalf("expected dim 4, got %d", len(result))
	}
	for i, v := range result {
		if math.Abs(float64(v-queryVec[i])) > 1e-6 {
			t.Fatalf("expected original query when no docs, got %v", result)
		}
	}
}

func TestRocchioNormalized(t *testing.T) {
	queryVec := []float32{1.0, 0.0}
	docVecs := [][]float32{{0.0, 1.0}}
	result := Rocchio(queryVec, docVecs, 0.5, 0.5)

	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)

	if math.Abs(norm-1.0) > 1e-4 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestRocchioSingleDoc(t *testing.T) {
	queryVec := []float32{1.0, 0.0}
	docVecs := [][]float32{{0.0, 1.0}}
	result := Rocchio(queryVec, docVecs, 0.6, 0.4)

	if result[0] <= 0 || result[1] <= 0 {
		t.Fatalf("expected both components > 0, got %v", result)
	}
}
