// Package link builds a document link graph and computes PageRank.
package link

// Graph maps docID → outgoing docIDs.
type Graph map[uint32][]uint32

// PageRank runs power iteration and returns per-doc scores that sum to ~1.
//
//	d=0.85 — standard damping factor (probability of following a link vs teleporting)
//	iters=30 — enough to converge on most corpora; check delta if unsure
func PageRank(graph Graph, numDocs int, d float64, iters int) []float64 {
	N := float64(numDocs)
	scores := make([]float64, numDocs)
	next := make([]float64, numDocs)

	// Uniform initialisation.
	for i := range scores {
		scores[i] = 1.0 / N
	}

	// Build in-link lists once — avoids O(N²) scan per iteration.
	inLinks := make([][]uint32, numDocs)
	for src, dests := range graph {
		for _, dest := range dests {
			if int(dest) < numDocs {
				inLinks[dest] = append(inLinks[dest], src)
			}
		}
	}

	// Precompute which nodes are dangling — avoids map lookup per iteration.
	isDangling := make([]bool, numDocs)
	for id := 0; id < numDocs; id++ {
		isDangling[id] = len(graph[uint32(id)]) == 0
	}

	for iter := 0; iter < iters; iter++ {
		var dangling float64
		for id := 0; id < numDocs; id++ {
			if isDangling[id] {
				dangling += scores[id]
			}
		}

		for id := 0; id < numDocs; id++ {
			// Teleport share + dangling redistribution.
			next[id] = (1-d)/N + d*dangling/N
			// Add rank from in-links.
			for _, src := range inLinks[uint32(id)] {
				outdeg := float64(len(graph[src]))
				if outdeg > 0 {
					next[id] += d * scores[src] / outdeg
				}
			}
		}
		copy(scores, next)
	}
	return scores
}

// BuildGraph constructs a Graph from the index using outgoing_link title fields.
// titleToID maps Wikipedia article titles to internal doc IDs.
// Links to unknown titles (redlinks) are silently ignored.
func BuildGraph(numDocs int, docLinks [][]string, titleToID map[string]uint32) Graph {
	graph := make(Graph, numDocs)
	for id := 0; id < numDocs; id++ {
		graph[uint32(id)] = nil // ensure dangling nodes are present
	}
	for id, links := range docLinks {
		for _, title := range links {
			if dest, ok := titleToID[title]; ok {
				graph[uint32(id)] = append(graph[uint32(id)], dest)
			}
		}
	}
	return graph
}
