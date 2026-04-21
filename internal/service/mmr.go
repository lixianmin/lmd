package service

import "math"

type MMRCandidate struct {
	ID        int64
	Embedding []float32
}

func SelectMMR(candidates []MMRCandidate, queryVec []float32, lambda float64, topK int) []MMRCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if topK > len(candidates) {
		topK = len(candidates)
	}

	var selected []MMRCandidate
	remaining := make([]MMRCandidate, len(candidates))
	copy(remaining, candidates)

	for len(selected) < topK {
		bestIdx := 0
		bestScore := -1e9

		for i, cand := range remaining {
			relevance := cosineSimilarity(cand.Embedding, queryVec)

			var maxSim float64
			for _, s := range selected {
				sim := cosineSimilarity(cand.Embedding, s.Embedding)
				if sim > maxSim {
					maxSim = sim
				}
			}

			score := lambda*relevance - (1-lambda)*maxSim
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
