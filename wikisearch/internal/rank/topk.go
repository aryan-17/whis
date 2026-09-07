package rank

import "container/heap"

// minHeap is a min-heap of Results by Score.
// The lowest score sits at the top so we can cheaply evict it when the heap exceeds k.
type minHeap []Result

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool  { return h[i].Score < h[j].Score }
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(Result)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// TopK returns the k highest-scoring Results sorted descending by score.
// Runs in O(n log k) — much cheaper than sorting all n results when k << n.
func TopK(results []Result, k int) []Result {
	if k <= 0 {
		return nil
	}
	h := &minHeap{}
	heap.Init(h)
	for _, r := range results {
		heap.Push(h, r)
		if h.Len() > k {
			heap.Pop(h) // evict the current lowest score
		}
	}
	// Drain heap into output slice, highest score last → reverse for descending order.
	out := make([]Result, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(Result)
	}
	return out
}
