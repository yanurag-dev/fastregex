// Package index implements the inverted index for regex query acceleration.
//
// The index is stored as three files in the index directory:
//
//	lookup.idx   — Fixed-size open-addressing hash table mapping trigrams to postings offsets.
//	             Each slot is 8 bytes: [trigram uint32][offset uint32].
//	             Load factor ≈ 0.67; slot value of 0 means empty.
//
//	postings.dat — Sequential posting list records, indexed by byte offsets in lookup.idx.
//	             Each record: [count uint32][fileID uint32 ... ].
//	             Posting lists are always sorted to enable fast intersection.
//
//	files.idx    — One filepath per line; line number (0-based) is the fileID.
//	             Allows decoder to map posting list fileIDs back to paths.
//
// The index preserves the no-false-negatives invariant: if a file matches a regex,
// it will always appear in the candidate set (false positives are acceptable).
//
// Query flow: trigram decomposition → hash table lookup → posting list intersection →
// candidate files → final regex engine run (only on candidates).
//
// See Builder for building, Writer for serialization, and Reader for querying.
package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unicode/utf8"

	"grepturbo/internal/posting"
	"grepturbo/internal/trigram"
)

const maxFileSize = 1 << 20 // 1 MB — skip files larger than this

// Builder walks a directory tree, extracts trigrams from each file,
// and accumulates an in-memory posting list (trigram → sorted fileID list).
type Builder struct {
	Posts   posting.List // trigram → sorted []fileID
	Files   []string     // fileID → filepath (index == fileID)
	RootDir string       // root directory of indexed tree
	Skip    []string     // additional directories to skip during walk
}

// NewBuilder creates a new Builder with empty postings and file list.
func NewBuilder() *Builder {
	return &Builder{
		Posts: make(posting.List),
	}
}

// Add indexes a single file by reading its content, extracting trigrams,
// and recording the trigram → fileID mappings in the posting list.
// Binary files (non-UTF-8) and files larger than 1 MB are skipped.
// Returns the fileID assigned to the file, or 0 if the file was skipped.
func (b *Builder) Add(path string) (uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Skip binary files — valid source files are UTF-8 text
	if !utf8.Valid(data) {
		return 0, nil
	}

	// Skip large files — they blow up the index with common trigrams
	if len(data) > maxFileSize {
		return 0, nil
	}

	fileID := uint32(len(b.Files))
	b.Files = append(b.Files, path)

	for _, t := range trigram.Extract(string(data)) {
		b.Posts.AddBatch(t, []uint32{fileID})
	}

	return fileID, nil
}

// defaultSkipDirs are directories that should never be indexed.
// These typically contain large, auto-generated, or version control files.
var defaultSkipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".hg":          true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".fastregex":   true,
}

type extractResult struct {
	path     string
	trigrams []trigram.T
}

// Build walks all files under rootDir and indexes each one concurrently.
// It sets b.RootDir and populates b.Files and b.Posts.
//
// Directories in defaultSkipDirs or in the skip argument are skipped entirely
// (e.g., "node_modules", ".git"). Binary files and files larger than 1 MB are
// also skipped. Errors reading individual files are silently ignored; only
// fatal walk errors are returned.
//
// Concurrency: uses GOMAXPROCS worker goroutines to read and extract trigrams,
// with a sequential collector to maintain a lock-free builder state.
func (b *Builder) Build(rootDir string, skip ...string) error {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	b.RootDir = absRoot
	b.Skip = skip

	skipSet := make(map[string]bool)
	for k, v := range defaultSkipDirs {
		skipSet[k] = v
	}
	for _, s := range skip {
		skipSet[s] = true
	}

	paths := make(chan string, 100)
	results := make(chan extractResult, 100)

	// Worker pool: read files and extract trigrams
	var wg sync.WaitGroup
	numWorkers := runtime.GOMAXPROCS(0)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range paths {
				data, err := os.ReadFile(path)
				if err != nil || !utf8.Valid(data) || len(data) > maxFileSize {
					continue
				}
				results <- extractResult{
					path:     path,
					trigrams: trigram.Extract(string(data)),
				}
			}
		}()
	}

	// Signal workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collector: update Builder state (sequential, lock-free)
	done := make(chan struct{})
	go func() {
		for res := range results {
			fileID := uint32(len(b.Files))
			b.Files = append(b.Files, res.path)
			for _, t := range res.trigrams {
				b.Posts.AddBatch(t, []uint32{fileID})
			}
		}
		close(done)
	}()

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipSet[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		paths <- path
		return nil
	})
	close(paths)

	if err != nil {
		return err
	}

	<-done
	b.Posts.Finalize()
	return nil
}
