package link_test

import (
	"math"
	"testing"

	"wikisearch/internal/link"
)

func TestPageRankSumsToOne(t *testing.T) {
	// 3-node graph: 0→1, 0→2, 1→2
	graph := link.Graph{
		0: []uint32{1, 2},
		1: []uint32{2},
		2: []uint32{},
	}
	scores := link.PageRank(graph, 3, 0.85, 30)
	var total float64
	for _, s := range scores {
		total += s
	}
	if math.Abs(total-1.0) > 0.01 {
		t.Errorf("scores sum to %f, want ~1.0", total)
	}
}

func TestPageRankMostLinkedHighest(t *testing.T) {
	// Node 2 receives links from both 0 and 1 — must rank highest.
	graph := link.Graph{
		0: []uint32{1, 2},
		1: []uint32{2},
		2: []uint32{},
	}
	scores := link.PageRank(graph, 3, 0.85, 30)
	if !(scores[2] > scores[1] && scores[2] > scores[0]) {
		t.Errorf("node 2 should have highest rank: %v", scores)
	}
}

func TestPageRankAllNonNegative(t *testing.T) {
	graph := link.Graph{
		0: []uint32{1},
		1: []uint32{0},
	}
	scores := link.PageRank(graph, 2, 0.85, 20)
	for i, s := range scores {
		if s < 0 {
			t.Errorf("score[%d] = %f, want >= 0", i, s)
		}
	}
}

func TestPageRankDanglingNode(t *testing.T) {
	// Node 1 has no outlinks (dangling) — rank must still be distributed.
	graph := link.Graph{
		0: []uint32{1},
		1: []uint32{}, // dangling
	}
	scores := link.PageRank(graph, 2, 0.85, 30)
	var total float64
	for _, s := range scores {
		total += s
	}
	if math.Abs(total-1.0) > 0.01 {
		t.Errorf("dangling node: scores sum to %f, want ~1.0", total)
	}
}
