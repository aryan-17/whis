package postings

// PhraseIntersect returns documents where term b appears exactly gap positions
// after term a. gap=1 means adjacent ("climate change").
// Both lists must have Positions populated (Sprint 6+ positional index).
func PhraseIntersect(a, b List, gap uint32) List {
	var out []Entry
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		if ai < bj {
			i++
		} else if ai > bj {
			j++
		} else {
			// Same doc — check if any position pair satisfies the gap.
			if hasPositionGap(a.Entries[i].Positions, b.Entries[j].Positions, gap) {
				out = append(out, Entry{DocID: ai, TermFreq: 1})
			}
			i++
			j++
		}
	}
	return List{DocFreq: uint32(len(out)), Entries: out}
}

// hasPositionGap returns true if any posA + gap == posB.
// Two-pointer over sorted position lists — O(m+n).
func hasPositionGap(posA, posB []uint32, gap uint32) bool {
	p, q := 0, 0
	for p < len(posA) && q < len(posB) {
		target := posA[p] + gap
		if posB[q] == target {
			return true
		} else if posB[q] < target {
			q++
		} else {
			p++
		}
	}
	return false
}
