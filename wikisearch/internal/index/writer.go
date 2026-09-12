package index

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"wikisearch/internal/postings"
)

// WriteSegment serialises mem to three files in dir:
//
//	segment.post — gap-encoded posting lists (varint)
//	segment.dict — sorted term dictionary with offsets into .post
//	segment.docs — doc metadata (JSON lines)
func WriteSegment(mem *MemoryIndex, dir string) error {
	terms := make([]string, 0, len(mem.postings))
	for t := range mem.postings {
		terms = append(terms, t)
	}
	sort.Strings(terms)

	// --- segment.post ---
	pf, err := os.Create(filepath.Join(dir, "segment.post"))
	if err != nil {
		return err
	}
	defer pf.Close()

	type dictEntry struct {
		term   string
		offset uint64
		length uint64
	}
	dictEntries := make([]dictEntry, 0, len(terms))
	var offset uint64

	for _, term := range terms {
		pl := mem.postings[term]
		var buf []byte
		buf = postings.AppendUvarint(buf, uint64(len(pl)))
		var prevID uint32
		for _, e := range pl {
			buf = postings.AppendUvarint(buf, uint64(e.DocID-prevID))
			buf = postings.AppendUvarint(buf, uint64(e.TermFreq))
			prevID = e.DocID
		}
		if _, err := pf.Write(buf); err != nil {
			return err
		}
		dictEntries = append(dictEntries, dictEntry{term, offset, uint64(len(buf))})
		offset += uint64(len(buf))
	}

	// --- segment.dict ---
	df, err := os.Create(filepath.Join(dir, "segment.dict"))
	if err != nil {
		return err
	}
	defer df.Close()

	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, uint32(len(dictEntries)))
	df.Write(hdr)

	tmp := make([]byte, 8)
	for _, de := range dictEntries {
		tb := []byte(de.term)
		binary.LittleEndian.PutUint16(tmp[:2], uint16(len(tb)))
		df.Write(tmp[:2])
		df.Write(tb)
		binary.LittleEndian.PutUint64(tmp, de.offset)
		df.Write(tmp)
		binary.LittleEndian.PutUint64(tmp, de.length)
		df.Write(tmp)
	}

	// --- segment.docs ---
	docf, err := os.Create(filepath.Join(dir, "segment.docs"))
	if err != nil {
		return err
	}
	defer docf.Close()

	type docRecord struct {
		Title  string `json:"t"`
		Length uint32 `json:"l"`
		Text   string `json:"x"`
	}
	enc := json.NewEncoder(docf)
	for _, doc := range mem.docs {
		if err := enc.Encode(docRecord{Title: doc.Title, Length: doc.Length, Text: doc.Text}); err != nil {
			return err
		}
	}
	return nil
}
