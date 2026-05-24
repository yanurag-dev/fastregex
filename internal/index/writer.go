package index

import (
	"bufio"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"

	"grepturbo/internal/trigram"
)

const slotSize = 8 // bytes per hash table slot: 4 bytes trigram + 4 bytes offset

// Write serializes a built index to three files in dir: postings.dat, lookup.idx, files.idx.
//
// Format details:
//
// postings.dat: Sequential posting list records. Each record:
//
//	[count uint32][fileID uint32][fileID uint32]...
//
// Byte offset of each record is stored in the lookup table.
// Posting lists are always sorted by fileID.
//
// lookup.idx: Fixed-size open-addressing hash table (8 bytes per slot).
// Each slot: [trigram uint32][offset uint32].
// A trigram value of 0 indicates an empty slot (trigram NUL NUL NUL never occurs).
// Load factor is maintained at ≈ 0.67 by using 1.5x numTrigrams slots.
// Linear probing resolves hash collisions.
// Footer contains numSlots as a 4-byte uint32.
//
// files.idx: Newline-separated filepath list. Line number (0-based) is the fileID.
//
// The write sequence is: postings → lookup table → files → metadata.
// All writes are flushed before metadata.json is created.
func Write(b *Builder, dir string) (err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Phase 1: Write postings.dat and collect trigram → byte offset mappings.
	// Buffered writes improve throughput by coalescing small syscalls.
	postingsPath := filepath.Join(dir, "postings.dat")
	pf, err := os.Create(postingsPath)
	if err != nil {
		return err
	}
	defer pf.Close()

	w := bufio.NewWriter(pf)

	// offsets maps each trigram to its starting byte offset in postings.dat.
	// This is later embedded in the hash table.
	offsets := make(map[trigram.T]uint32)
	var cursor uint32

	buf := make([]byte, 4)

	for t, ids := range b.Posts {
		offsets[t] = cursor

		// Write posting list count, then each fileID (as uint32 LE).
		binary.LittleEndian.PutUint32(buf, uint32(len(ids)))
		if _, err := w.Write(buf); err != nil {
			return err
		}
		cursor += 4

		for _, id := range ids {
			binary.LittleEndian.PutUint32(buf, id)
			if _, err := w.Write(buf); err != nil {
				return err
			}
			cursor += 4
		}
	}

	// Flush the buffered writer to ensure all postings data is written to disk.
	// This must happen before we start writing lookup.idx, since we need the
	// final file offsets to be stable.
	if err := w.Flush(); err != nil {
		return err
	}

	// Phase 2: Build and write lookup.idx hash table (open-addressing).
	// The table maps trigrams to byte offsets in postings.dat.
	// Size: 1.5x unique trigrams to maintain load factor ≈ 0.67 (short probe chains).
	numSlots := uint32(len(offsets)*3/2) + 1

	lookupPath := filepath.Join(dir, "lookup.idx")
	lf, err := os.Create(lookupPath)
	if err != nil {
		return err
	}
	defer lf.Close()

	// Allocate table in memory; each slot is 8 bytes.
	// Format per slot: [trigramValue uint32][postingsOffset uint32].
	// Empty slots have trigramValue == 0 (safe: trigram NUL NUL NUL never appears).
	table := make([]byte, numSlots*slotSize)

	for t, off := range offsets {
		slot := uint32(t) % numSlots

		// Linear probe to find the next empty slot.
		// Collisions are acceptable and expected at load factor 0.67.
		for {
			base := slot * slotSize
			if binary.LittleEndian.Uint32(table[base:]) == 0 {
				binary.LittleEndian.PutUint32(table[base:], uint32(t))
				binary.LittleEndian.PutUint32(table[base+4:], off)
				break
			}
			slot = (slot + 1) % numSlots
		}
	}

	if _, err := lf.Write(table); err != nil {
		return err
	}

	// Write numSlots as a 4-byte footer so the reader can determine table size.
	binary.LittleEndian.PutUint32(buf, numSlots)
	if _, err := lf.Write(buf); err != nil {
		return err
	}

	// Phase 3: Write files.idx (fileID → filepath mapping).
	// Newline-separated; line number (0-based) is the fileID.
	filesPath := filepath.Join(dir, "files.idx")
	ff, err := os.Create(filesPath)
	if err != nil {
		return err
	}
	defer ff.Close()

	if _, err := ff.WriteString(strings.Join(b.Files, "\n")); err != nil {
		return err
	}

	// Write metadata last, after all index files are stable on disk.
	return WriteMetadata(dir, b.RootDir, b.Skip)
}
