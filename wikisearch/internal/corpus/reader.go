package corpus

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// rawDoc matches the JSON fields we care about from each document line.
type rawDoc struct {
	Title        string   `json:"title"`
	Text         string   `json:"text"`
	OutgoingLink []string `json:"outgoing_link"`
}

// Reader streams Documents from a CirrusSearch dump (.json.gz or .json.bz2).
// The dump is line-delimited JSON in pairs: action line (odd), document line (even).
// Reader skips action lines and assigns sequential IDs starting from 0.
type Reader struct {
	f      *os.File
	sc     *bufio.Scanner
	nextID uint32
	line   int
}

// NewReader opens the dump at path.
// Compression is detected by extension: .bz2 → bzip2, anything else → gzip.
func NewReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dump: %w", err)
	}

	var raw io.Reader
	if strings.HasSuffix(path, ".bz2") {
		raw = bzip2.NewReader(f)
	} else {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		raw = gz
	}

	sc := bufio.NewScanner(raw)
	// Some Wikipedia articles exceed the 64 KB default scanner buffer.
	sc.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	return &Reader{f: f, sc: sc}, nil
}

// Next returns the next Document from the stream.
// ok is false when the stream is exhausted with no error.
func (r *Reader) Next() (Document, bool, error) {
	for {
		if !r.sc.Scan() {
			if err := r.sc.Err(); err != nil {
				return Document{}, false, fmt.Errorf("scan: %w", err)
			}
			return Document{}, false, nil
		}
		r.line++
		if r.line%2 == 1 {
			// Odd lines are action lines (e.g. {"index":{"_id":"..."}}) — skip.
			continue
		}

		var raw rawDoc
		if err := json.Unmarshal(r.sc.Bytes(), &raw); err != nil {
			return Document{}, false, fmt.Errorf("line %d: %w", r.line, err)
		}

		doc := Document{
			ID:    r.nextID,
			Title: raw.Title,
			Text:  raw.Text,
			Links: raw.OutgoingLink,
		}
		r.nextID++
		return doc, true, nil
	}
}

// Close releases the underlying file.
func (r *Reader) Close() error {
	return r.f.Close()
}
