package postings

// Intersect returns entries present in both a and b (AND), sorted by DocID.
// Two-pointer merge — O(n+m), no hashing.
func Intersect(a, b List) List {
	// Upper bound: result can't exceed the shorter list.
	capacity := len(a.Entries)
	if len(b.Entries) < capacity {
		capacity = len(b.Entries)
	}
	out := make([]Entry, 0, capacity)
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		switch {
		case ai == bj:
			out = append(out, Entry{
				DocID:    ai,
				TermFreq: a.Entries[i].TermFreq + b.Entries[j].TermFreq,
			})
			i++
			j++
		case ai < bj:
			i++
		default:
			j++
		}
	}
	return List{DocFreq: uint32(len(out)), Entries: out}
}

// Union returns all entries in a or b (OR), sorted by DocID.
func Union(a, b List) List {
	out := make([]Entry, 0, len(a.Entries)+len(b.Entries))
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		switch {
		case ai == bj:
			out = append(out, Entry{
				DocID:    ai,
				TermFreq: a.Entries[i].TermFreq + b.Entries[j].TermFreq,
			})
			i++
			j++
		case ai < bj:
			out = append(out, a.Entries[i])
			i++
		default:
			out = append(out, b.Entries[j])
			j++
		}
	}
	out = append(out, a.Entries[i:]...)
	out = append(out, b.Entries[j:]...)
	return List{DocFreq: uint32(len(out)), Entries: out}
}

// Difference returns entries in a but not in b (NOT b), sorted by DocID.
func Difference(a, b List) List {
	out := make([]Entry, 0, len(a.Entries))
	i, j := 0, 0
	for i < len(a.Entries) {
		if j >= len(b.Entries) || a.Entries[i].DocID < b.Entries[j].DocID {
			out = append(out, a.Entries[i])
			i++
		} else if a.Entries[i].DocID == b.Entries[j].DocID {
			i++
			j++
		} else {
			j++
		}
	}
	return List{DocFreq: uint32(len(out)), Entries: out}
}
