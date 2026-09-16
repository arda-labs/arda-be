package knowledge

import "testing"

// The hybrid retrieval ranking contract: RRF (k=60) with deterministic tie
// order. This is the gate that keeps ranking stable in CI without a database
// or an embedding provider.

func TestFuseRRFCombinesBothLegs(t *testing.T) {
	vectorRanks := []rankedID{{chunkID: "both", rank: 1}, {chunkID: "vector-only", rank: 2}}
	ftsRanks := []rankedID{{chunkID: "both", rank: 2}, {chunkID: "fts-only", rank: 1}}

	got := fuseRRF(vectorRanks, ftsRanks, 0)

	if len(got) != 3 {
		t.Fatalf("expected 3 fused chunks, got %d", len(got))
	}
	if got[0].chunkID != "both" {
		t.Fatalf("a chunk ranked by both legs must win, got order %v", got)
	}
	// "both" = 1/61 + 1/62 > "fts-only" = 1/61 > "vector-only" = 1/62.
	if got[1].chunkID != "fts-only" || got[2].chunkID != "vector-only" {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestFuseRRFRankWeighting(t *testing.T) {
	got := fuseRRF(
		[]rankedID{{chunkID: "rank-1", rank: 1}, {chunkID: "rank-3", rank: 3}},
		nil,
		0,
	)
	if got[0].chunkID != "rank-1" || got[1].chunkID != "rank-3" {
		t.Fatalf("higher rank must score higher: %v", got)
	}
}

func TestFuseRRFBoundsAndEmpty(t *testing.T) {
	if got := fuseRRF(nil, nil, 5); len(got) != 0 {
		t.Fatalf("empty legs must fuse to nothing, got %v", got)
	}

	got := fuseRRF(
		[]rankedID{{chunkID: "a", rank: 1}, {chunkID: "b", rank: 2}, {chunkID: "c", rank: 3}},
		nil,
		2,
	)
	if len(got) != 2 || got[0].chunkID != "a" || got[1].chunkID != "b" {
		t.Fatalf("topK must truncate the tail: %v", got)
	}
}

// Identical fused scores must keep the vector leg's order so ranking is stable
// across map iteration (the reason fuseRRF orders explicitly).
func TestFuseRRFDeterministicTies(t *testing.T) {
	vector := []rankedID{{chunkID: "v-first", rank: 2}}
	fts := []rankedID{{chunkID: "f-second", rank: 2}}

	first := fuseRRF(vector, fts, 0)
	second := fuseRRF(vector, fts, 0)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected 2 chunks, got %v / %v", first, second)
	}
	if first[0].chunkID != second[0].chunkID || first[0].chunkID != "v-first" {
		t.Fatalf("ties must keep the vector leg's order every run: %v vs %v", first, second)
	}
}
