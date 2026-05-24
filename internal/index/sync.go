// This file implements git-based incremental index updates (part of the index package).
//
// Sync Strategy (Baseline + Overlay):
// The index starts with a baseline built at a specific git commit. After git operations
// add, modify, or delete files, Sync() detects these changes and builds an in-memory overlay:
//
// - Baseline: the on-disk index (lookup.idx, postings.dat, files.idx) at a known commit
// - Overlay: in-memory posting lists for modified and untracked files
// - Tombstones: a set of paths to ignore from the baseline (deleted or superceded by overlay)
//
// Search queries intersect baseline and overlay posting lists separately, then union
// the results. This allows searches to include uncommitted changes without rebuilding
// the full index.
//
// Commit Drift: If the baseline commit differs from HEAD, Sync returns drift=true.
// The search must be aborted; the user should rebuild the index.
package index

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"grepturbo/internal/posting"
	"grepturbo/internal/trigram"
)

// GitStatus categorizes files based on their git status relative to the baseline commit.
type GitStatus struct {
	Modified  []string // files with changes (M) or additions (A) in the index or working tree
	Untracked []string // files not tracked by git (??)
	Deleted   []string // files deleted from the index or working tree (D)
}

// Overlay holds an in-memory index of files modified since the baseline was built.
// It is merged with the baseline during search to include uncommitted changes.
type Overlay struct {
	Posts      posting.List        // in-memory posting lists for dirty files
	Files      []string            // dirty files (fileID → filepath); IDs start from len(baseline.Files)
	Tombstones map[string]bool     // baseline paths to hide (deleted or superceded by overlay)
}

// Sync builds an in-memory overlay of dirty files and detects commit drift.
//
// If the repository is clean or git is unavailable, returns an empty overlay with no drift.
// If committed files have changed since the baseline, returns drift=true (index is stale).
// Otherwise, indexes modified and untracked files, marks deleted files as tombstones,
// and returns the overlay for merging with the baseline during search.
func (r *Reader) Sync() (*Overlay, bool, error) {
	current, err := CurrentCommit(r.Meta.RootDir)
	if err != nil {
		// Not in a git repo (or git not installed) — nothing to sync.
		return &Overlay{
			Posts:      make(posting.List),
			Tombstones: make(map[string]bool),
		}, false, nil
	}

	// Commit Drift detected
	if r.Meta.Commit != current && r.Meta.Commit != "unknown" {
		return nil, true, nil
	}

	status, err := GetGitStatus(r.Meta.RootDir)
	if err != nil {
		return nil, false, err
	}

	overlay := &Overlay{
		Posts:      make(posting.List),
		Tombstones: make(map[string]bool),
	}

	// Deleted files are Tombstones
	for _, p := range status.Deleted {
		overlay.Tombstones[filepath.Join(r.Meta.RootDir, p)] = true
	}

	// Modified files are both Tombstones (hide old version) and Indexed (show new version)
	dirtyFiles := append(status.Modified, status.Untracked...)
	for _, p := range status.Modified {
		overlay.Tombstones[filepath.Join(r.Meta.RootDir, p)] = true
	}

	// Index dirty files in memory
	for _, relPath := range dirtyFiles {
		absPath := filepath.Join(r.Meta.RootDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue // skip files we can't read
		}
		if !utf8.Valid(data) || len(data) > maxFileSize {
			continue
		}

		fileID := uint32(len(r.Files) + len(overlay.Files))
		overlay.Files = append(overlay.Files, absPath)

		for _, t := range trigram.Extract(string(data)) {
			overlay.Posts.AddBatch(t, []uint32{fileID})
		}
	}
	overlay.Posts.Finalize()

	return overlay, false, nil
}

// GetGitStatus runs git status --porcelain and categorizes files by their state.
// Modified and added files are grouped together; untracked files are separate.
func GetGitStatus(dir string) (*GitStatus, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	status := &GitStatus{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}

		// git status --porcelain format: "XY PATH"
		xy := line[:2]
		path := line[3:]

		switch {
		case xy == "??":
			status.Untracked = append(status.Untracked, path)
		case strings.Contains(xy, "D"):
			status.Deleted = append(status.Deleted, path)
		case strings.Contains(xy, "M") || strings.Contains(xy, "A"):
			status.Modified = append(status.Modified, path)
		}
	}

	return status, scanner.Err()
}
