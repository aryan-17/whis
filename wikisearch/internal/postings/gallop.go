package postings

// IntersectGallop is like Intersect but uses exponential (galloping) search
// to advance the shorter list when gaps are large. Faster when lists have
// very different densities or large contiguous runs without overlap.
//
// For dense lists of similar size, plain Intersect is equally fast.
func IntersectGallop(a, b List) List {
	// Always gallop on the longer list to maximise jump distance.
	if len(a.Entries) > len(b.Entries) {
		a, b = b, a
	}
	var out []Entry
	j := 0
	for _, ae := range a.Entries {
		// Advance j in b to first entry >= ae.DocID using galloping search.
		j = gallopTo(b.Entries, ae.DocID, j)
		if j >= len(b.Entries) {
			break
		}
		if b.Entries[j].DocID == ae.DocID {
			out = append(out, Entry{
				DocID:    ae.DocID,
				TermFreq: ae.TermFreq + b.Entries[j].TermFreq,
			})
			j++
		}
	}
	return List{DocFreq: uint32(len(out)), Entries: out}
}

// gallopTo returns the smallest index i >= start such that entries[i].DocID >= target.
// Uses exponential probing then binary search — O(log n) rather than O(n).
func gallopTo(entries []Entry, target uint32, start int) int {
	if start >= len(entries) || entries[start].DocID >= target {
		return start
	}
	// Exponential probing: double the step until we overshoot.
	step := 1
	lo := start
	hi := start + step
	for hi < len(entries) && entries[hi].DocID < target {
		lo = hi
		step *= 2
		hi = lo + step
	}
	if hi > len(entries) {
		hi = len(entries)
	}
	// Binary search in [lo, hi).
	for lo < hi {
		mid := (lo + hi) / 2
		if entries[mid].DocID < target {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
