package query

import (
	"bufio"
	"fmt"
	"os"
	"regexp"

	"grepturbo/internal/index"
	"grepturbo/internal/posting"
)

// Match represents a single regex match on a line within a file.
type Match struct {
	File string // absolute path to the file
	Line int    // 1-indexed line number
	Text string // the matched line content
}

// ErrCommitDrift indicates that the index baseline commit differs from the current HEAD.
// The index must be rebuilt to search the current state of the repository.
type ErrCommitDrift struct {
	Baseline string // commit hash when the index was built
	Current  string // current HEAD commit hash
}

func (e *ErrCommitDrift) Error() string {
	return fmt.Sprintf("commit drift: index is at %s, but HEAD is at %s", truncate(e.Baseline, 7), truncate(e.Current, 7))
}

func truncate(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// Search executes a regex pattern against an index, returning all matching lines.
//
// The search pipeline:
// 1. Sync with Git to include modified/untracked files and hide deleted files
// 2. Decompose the regex into required trigrams
// 3. Intersect baseline and overlay posting lists to find candidate files
// 4. Run the compiled regex against each candidate file
//
// Results are guaranteed to have no false negatives: if a file matches the pattern,
// it will appear in the results.
// Returns ErrCommitDrift if the index baseline diverges from the current HEAD.
func Search(r *index.Reader, pattern string) ([]Match, error) {
	// ── Step 0: Sync with Git (Baseline + Overlay) ──────────────────────────
	overlay, drift, err := r.Sync()
	if err != nil {
		return nil, fmt.Errorf("sync error: %w", err)
	}
	if drift {
		current, err := index.CurrentCommit(r.Meta.RootDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get current commit: %w", err)
		}
		return nil, &ErrCommitDrift{Baseline: r.Meta.Commit, Current: current}
	}

	// ── Step 1: decompose regex into trigrams ────────────────────────────────
	decomp, err := Decompose(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	// ── Step 2: compile the real regex ──────────────────────────────────────
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}

	// ── Step 3: determine candidate files ───────────────────────────────────
	var candidates []string

	if decomp.Wildcard || len(decomp.Trigrams) == 0 {
		// No trigrams extracted — must scan every file
		// Filter out deleted files (Tombstones) from Baseline
		for _, p := range r.Files {
			if !overlay.Tombstones[p] {
				candidates = append(candidates, p)
			}
		}
		// Add all Overlay files
		candidates = append(candidates, overlay.Files...)
	} else {
		// 3A: Baseline Candidates
		var baselineLists [][]uint32
		missingInBaseline := false
		for _, t := range decomp.Trigrams {
			ids, err := r.Lookup(t)
			if err != nil {
				return nil, fmt.Errorf("lookup trigram %s: %w", t, err)
			}
			if len(ids) == 0 {
				missingInBaseline = true
				break
			}
			baselineLists = append(baselineLists, ids)
		}

		if !missingInBaseline {
			fileIDs := posting.Intersect(baselineLists...)
			for _, id := range fileIDs {
				if int(id) < len(r.Files) {
					path := r.Files[id]
					if !overlay.Tombstones[path] {
						candidates = append(candidates, path)
					}
				}
			}
		}

		// 3B: Overlay Candidates
		var overlayLists [][]uint32
		missingInOverlay := false
		for _, t := range decomp.Trigrams {
			ids := overlay.Posts.Get(t)
			if len(ids) == 0 {
				missingInOverlay = true
				break
			}
			overlayLists = append(overlayLists, ids)
		}

		if !missingInOverlay {
			fileIDs := posting.Intersect(overlayLists...)
			for _, id := range fileIDs {
				// Overlay IDs start from len(r.Files)
				idx := int(id) - len(r.Files)
				if idx >= 0 && idx < len(overlay.Files) {
					candidates = append(candidates, overlay.Files[idx])
				}
			}
		}
	}

	// ── Step 4: run regex on candidate files only ────────────────────────────
	var matches []Match
	for _, path := range candidates {
		ms, err := grep(re, path)
		if err != nil {
			// File may have been deleted since indexing — skip it
			continue
		}
		matches = append(matches, ms...)
	}

	return matches, nil
}

// grep scans a file line-by-line and returns all lines matching the compiled regex.
// Returns an error only if the file cannot be read; missing files are silently skipped.
func grep(re *regexp.Regexp, path string) ([]Match, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []Match
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, Match{
				File: path,
				Line: lineNum,
				Text: line,
			})
		}
	}

	return matches, scanner.Err()
}
