package postings

// AppendUvarint encodes x as a variable-length unsigned integer and appends to buf.
// Each byte uses 7 bits of value; bit 8 signals "more bytes follow".
// Values < 128 encode in 1 byte. Values < 16384 in 2 bytes. Etc.
// This is why delta-encoded sorted docIDs compress well: small gaps → small varints.
func AppendUvarint(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = append(buf, byte(x)|0x80) // emit 7 bits with continuation bit set
		x >>= 7
	}
	return append(buf, byte(x)) // final byte, continuation bit clear
}

// ReadUvarint decodes a varint from buf starting at offset.
// Returns the decoded value and the number of bytes consumed.
// Returns n=-1 if the buffer is truncated.
func ReadUvarint(buf []byte, offset int) (uint64, int) {
	var x uint64
	var shift uint
	for i := offset; i < len(buf); i++ {
		b := buf[i]
		x |= uint64(b&0x7F) << shift
		shift += 7
		if b < 0x80 {
			return x, i - offset + 1
		}
	}
	return 0, -1 // truncated
}
