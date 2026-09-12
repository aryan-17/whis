package index

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"wikisearch/internal/corpus"
	"wikisearch/internal/postings"
)

// segmentIndex reads from on-disk segment files and satisfies Index.
type segmentIndex struct {
	dict     map[string][2]uint64 // term → {postOffset, postLen}
	postData []byte
	docs     []corpus.Document
	docLens  []uint32
	totalLen uint64
}

// OpenSegment reads the three segment files from dir and returns an Index.
func OpenSegment(dir string) (Index, error) {
	s := &segmentIndex{dict: make(map[string][2]uint64)}

	// --- segment.dict ---
	dictData, err := os.ReadFile(filepath.Join(dir, "segment.dict"))
	if err != nil {
		return nil, fmt.Errorf("read dict: %w", err)
	}
	pos := 0
	numTerms := int(binary.LittleEndian.Uint32(dictData[pos:]))
	pos += 4
	for i := 0; i < numTerms; i++ {
		tLen := int(binary.LittleEndian.Uint16(dictData[pos:]))
		pos += 2
		term := string(dictData[pos : pos+tLen])
		pos += tLen
		off := binary.LittleEndian.Uint64(dictData[pos:])
		pos += 8
		length := binary.LittleEndian.Uint64(dictData[pos:])
		pos += 8
		s.dict[term] = [2]uint64{off, length}
	}

	// --- segment.post ---
	s.postData, err = os.ReadFile(filepath.Join(dir, "segment.post"))
	if err != nil {
		return nil, fmt.Errorf("read post: %w", err)
	}

	// --- segment.docs ---
	docf, err := os.Open(filepath.Join(dir, "segment.docs"))
	if err != nil {
		return nil, fmt.Errorf("read docs: %w", err)
	}
	defer docf.Close()

	type docRecord struct {
		Title  string `json:"t"`
		Length uint32 `json:"l"`
		Text   string `json:"x"`
	}
	sc := bufio.NewScanner(docf)
	sc.Buffer(make([]byte, 1024*1024), 10*1024*1024) // articles can be large
	var id uint32
	for sc.Scan() {
		var rec docRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("docs line %d: %w", id, err)
		}
		s.docs = append(s.docs, corpus.Document{ID: id, Title: rec.Title, Length: rec.Length, Text: rec.Text})
		s.docLens = append(s.docLens, rec.Length)
		s.totalLen += uint64(rec.Length)
		id++
	}
	return s, nil
}

// Lookup decodes the posting list for term on demand.
func (s *segmentIndex) Lookup(term string) (postings.List, bool) {
	loc, ok := s.dict[term]
	if !ok {
		return postings.List{}, false
	}
	data := s.postData[loc[0] : loc[0]+loc[1]]

	docFreq, n := postings.ReadUvarint(data, 0)
	off := n
	entries := make([]postings.Entry, docFreq)
	var prevID uint32
	for i := range entries {
		gap, n1 := postings.ReadUvarint(data, off)
		off += n1
		tf, n2 := postings.ReadUvarint(data, off)
		off += n2
		prevID += uint32(gap)
		entries[i] = postings.Entry{DocID: prevID, TermFreq: uint32(tf)}
	}
	return postings.List{Term: term, DocFreq: uint32(docFreq), Entries: entries}, true
}

func (s *segmentIndex) NumDocs() uint32         { return uint32(len(s.docs)) }
func (s *segmentIndex) DocLen(id uint32) uint32 { return s.docLens[id] }
func (s *segmentIndex) AvgDocLen() float64 {
	if len(s.docs) == 0 {
		return 0
	}
	return float64(s.totalLen) / float64(len(s.docs))
}
func (s *segmentIndex) Doc(id uint32) (corpus.Document, error) {
	if int(id) >= len(s.docs) {
		return corpus.Document{}, fmt.Errorf("doc %d out of range", id)
	}
	return s.docs[id], nil
}
