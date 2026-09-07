package postings_test

import (
	"testing"

	"wikisearch/internal/postings"
)

func TestVarintRoundtrip(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 255, 16383, 16384, 1<<21 - 1, 1<<28 - 1, 1<<32 - 1}
	for _, v := range cases {
		buf := postings.AppendUvarint(nil, v)
		got, n := postings.ReadUvarint(buf, 0)
		if got != v {
			t.Errorf("roundtrip(%d): got %d", v, got)
		}
		if n <= 0 {
			t.Errorf("ReadUvarint(%d): n=%d", v, n)
		}
	}
}

func TestVarintSmallNumberOneByte(t *testing.T) {
	// Values < 128 must fit in exactly 1 byte.
	for _, v := range []uint64{0, 1, 64, 127} {
		buf := postings.AppendUvarint(nil, v)
		if len(buf) != 1 {
			t.Errorf("value %d encoded in %d bytes, want 1", v, len(buf))
		}
	}
}

func TestVarintLargeNumberMultipleBytes(t *testing.T) {
	buf := postings.AppendUvarint(nil, 128)
	if len(buf) < 2 {
		t.Errorf("value 128 encoded in %d bytes, want >= 2", len(buf))
	}
}

func TestVarintSequential(t *testing.T) {
	// Encode multiple varints back to back, decode sequentially.
	var buf []byte
	vals := []uint64{300, 1, 16500, 0, 99999}
	for _, v := range vals {
		buf = postings.AppendUvarint(buf, v)
	}
	off := 0
	for _, want := range vals {
		got, n := postings.ReadUvarint(buf, off)
		if got != want {
			t.Errorf("sequential: got %d, want %d at offset %d", got, want, off)
		}
		off += n
	}
}
