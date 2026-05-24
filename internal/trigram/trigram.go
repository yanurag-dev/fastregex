// Package trigram provides trigram extraction and manipulation for trigram-based indexing.
//
// A trigram is a 3-character sequence packed into a uint32 for efficient indexing.
// The package supports extracting all overlapping trigrams from strings and converting
// between packed (uint32) and unpacked (3 bytes) representations.
//
// Trigrams are the foundation of GrepTurbo's inverted index: each unique trigram maps
// to a posting list of file IDs that contain it. Regex patterns are decomposed into
// their required trigrams to avoid scanning files that cannot possibly match.
package trigram

// T represents a trigram as a packed 3-byte uint32.
// Bytes are packed as: byte0<<16 | byte1<<8 | byte2.
// For example, the string "abc" is packed as:
//
//	T = (byte('a') << 16) | (byte('b') << 8) | byte('c')
type T uint32

// FromBytes packs three bytes into a trigram.
func FromBytes(a, b, c byte) T {
	return T(uint32(a)<<16 | uint32(b)<<8 | uint32(c))
}

// Bytes unpacks a trigram into its three bytes.
func (t T) Bytes() (byte, byte, byte) {
	return byte(t >> 16), byte(t >> 8), byte(t)
}

// String returns the trigram as a 3-character string.
func (t T) String() string {
	a, b, c := t.Bytes()
	return string([]byte{a, b, c})
}

// Extract returns all overlapping trigrams from s, deduplicated and in order of first appearance.
// Strings shorter than 3 characters return nil.
// For example, Extract("banana") returns trigrams "ban", "ana", "nan" (without duplicates).
func Extract(s string) []T {
	if len(s) < 3 {
		return nil
	}
	seen := make(map[T]struct{})
	out := make([]T, 0, len(s)-2)
	for i := 0; i <= len(s)-3; i++ {
		t := FromBytes(s[i], s[i+1], s[i+2])
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// ExtractWithDuplicates returns all overlapping trigrams preserving order, including duplicates.
// Strings shorter than 3 characters return nil.
// This is used internally when building posting lists where duplicate trigrams must be preserved
// to account for multiple occurrences in the same file.
func ExtractWithDuplicates(s string) []T {
	if len(s) < 3 {
		return nil
	}
	out := make([]T, 0, len(s)-2)
	for i := 0; i <= len(s)-3; i++ {
		out = append(out, FromBytes(s[i], s[i+1], s[i+2]))
	}
	return out
}
