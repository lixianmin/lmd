package service

import "math"

func Rocchio(queryVec []float32, docVecs [][]float32, alpha, beta float64) []float32 {
	dim := len(queryVec)
	result := make([]float32, dim)

	for i, v := range queryVec {
		result[i] = float32(alpha) * v
	}

	if len(docVecs) > 0 {
		avg := make([]float32, dim)
		for _, doc := range docVecs {
			for i, v := range doc {
				avg[i] += v
			}
		}
		for i := range avg {
			avg[i] /= float32(len(docVecs))
			result[i] += float32(beta) * avg[i]
		}
	}

	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range result {
			result[i] /= float32(norm)
		}
	}

	return result
}
