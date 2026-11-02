package live

import "testing"

func TestCosineSimilarityAndSemanticRanking(t *testing.T) {
	if cosineSimilarity([]float32{1, 0}, []float32{1, 0}) < 0.999 {
		t.Fatal("identical vectors should score 1")
	}
	if cosineSimilarity([]float32{1, 0}, []float32{0, 1}) > 0.001 {
		t.Fatal("orthogonal vectors should score 0")
	}
	items := []memoryCandidate{
		{Category: "profile", Content: "coffee", Importance: 5, Embedding: []float32{0, 1}},
		{Category: "profile", Content: "deploy", Importance: 1, Embedding: []float32{1, 0}},
	}
	ranked := rankMemoryCandidatesSemantic(items, []float32{1, 0}, 1)
	if len(ranked) != 1 || ranked[0].Content != "deploy" {
		t.Fatalf("semantic ranking failed: %#v", ranked)
	}
}
