package index

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"grepturbo/internal/trigram"
)

// Reader provides read-only access to a built index.
// The lookup table is mmap'd into memory; postings are read on-demand via
// random access to postings.dat. Call NewReader to open and Close when done.
//
// Lookups preserve the no-false-negatives invariant: if a file matches a regex,
// it will always be present in the candidate set returned by Lookup.
type Reader struct {
	table    []byte    // mmap'd lookup.idx — fixed-size hash table
	numSlots uint32    // number of slots in the hash table
	postings *os.File  // open handle to postings.dat for random reads
	Files    []string  // fileID → filepath mapping from files.idx
	Meta     *Metadata // index metadata (commit, rootDir, skip dirs)
}

// NewReader opens an index in dir and returns a ready-to-query Reader.
// It loads metadata, mmap's the lookup table (PROT_READ, MAP_SHARED),
// and opens postings.dat for random access. Close must be called when done
// to unmap the lookup table and close the postings file.
func NewReader(dir string) (*Reader, error) {
	// Load metadata.json (commit, root directory, skip directories).
	meta, err := ReadMetadata(dir)
	if err != nil {
		return nil, fmt.Errorf("read metadata.json: %w", err)
	}

	// Open and mmap lookup.idx (hash table).
	// The file contains numSlots as the 4-byte footer.
	lookupPath := filepath.Join(dir, "lookup.idx")
	lf, err := os.Open(lookupPath)
	if err != nil {
		return nil, fmt.Errorf("open lookup.idx: %w", err)
	}
	defer lf.Close()

	info, err := lf.Stat()
	if err != nil {
		return nil, err
	}
	size := int(info.Size())
	if size < 4 {
		return nil, fmt.Errorf("lookup.idx too small")
	}

	// Mmap the lookup table with PROT_READ (read-only) and MAP_SHARED.
	// This allows the OS to page in only the needed hash table entries.
	table, err := unix.Mmap(int(lf.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap lookup.idx: %w", err)
	}

	// Extract numSlots from the 4-byte footer.
	numSlots := binary.LittleEndian.Uint32(table[size-4:])

	// Open postings.dat for random access (Lookup → readPostings uses ReadAt).
	postingsPath := filepath.Join(dir, "postings.dat")
	pf, err := os.Open(postingsPath)
	if err != nil {
		unix.Munmap(table)
		return nil, fmt.Errorf("open postings.dat: %w", err)
	}

	// Load files.idx (fileID → filepath).
	filesPath := filepath.Join(dir, "files.idx")
	data, err := os.ReadFile(filesPath)
	if err != nil {
		unix.Munmap(table)
		pf.Close()
		return nil, fmt.Errorf("open files.idx: %w", err)
	}

	var files []string
	if len(data) > 0 {
		files = strings.Split(string(data), "\n")
	}

	return &Reader{
		table:    table,
		numSlots: numSlots,
		postings: pf,
		Files:    files,
		Meta:     meta,
	}, nil
}

// Lookup returns the sorted file IDs for trigram t, or nil if not found.
// It performs a linear probe through the hash table to find the trigram,
// then reads the corresponding posting list from postings.dat.
// The returned slice is nil (not empty) if the trigram is not in the index.
//
// Lookup is safe to call concurrently. It does not modify Reader state.
func (r *Reader) Lookup(t trigram.T) ([]uint32, error) {
	if r.numSlots == 0 {
		return nil, nil
	}

	slot := uint32(t) % r.numSlots

	// Linear probe the hash table until we find the trigram or an empty slot.
	for {
		base := slot * slotSize
		stored := binary.LittleEndian.Uint32(r.table[base:])

		if stored == 0 {
			// Empty slot — trigram was never indexed.
			return nil, nil
		}

		if trigram.T(stored) == t {
			// Match found — read and return the posting list at this offset.
			offset := int64(binary.LittleEndian.Uint32(r.table[base+4:]))
			return r.readPostings(offset)
		}

		// Hash collision — probe the next slot.
		slot = (slot + 1) % r.numSlots
	}
}

// readPostings reads a posting list from postings.dat at the given byte offset.
// Format: [count uint32][fileID uint32 ... ].
// Returns nil (not empty slice) if count is 0.
func (r *Reader) readPostings(offset int64) ([]uint32, error) {
	buf := make([]byte, 4)

	if _, err := r.postings.ReadAt(buf, offset); err != nil {
		return nil, fmt.Errorf("read postings count at %d: %w", offset, err)
	}
	count := binary.LittleEndian.Uint32(buf)

	if count == 0 {
		return nil, nil
	}

	ids := make([]uint32, count)
	data := make([]byte, count*4)
	if _, err := r.postings.ReadAt(data, offset+4); err != nil {
		return nil, fmt.Errorf("read postings data at %d: %w", offset+4, err)
	}
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	return ids, nil
}

// Close unmaps the lookup table and closes the postings file.
// After Close, the Reader is no longer usable.
func (r *Reader) Close() error {
	r.postings.Close()
	return unix.Munmap(r.table)
}
