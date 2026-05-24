package index

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Metadata stores baseline index metadata, including the git commit at build time.
// It is used to detect commit drift (stale index) during sync operations.
type Metadata struct {
	Commit  string   `json:"commit"` // git commit hash when the index was built
	RootDir string   `json:"root_dir"` // root directory of the indexed repository
	Skip    []string `json:"skip"`   // glob patterns to exclude from the index
}

// WriteMetadata writes index metadata to metadata.json, capturing the current commit.
func WriteMetadata(dir, rootDir string, skip []string) error {
	commit, err := CurrentCommit(rootDir)
	if err != nil {
		commit = "unknown"
	}

	m := Metadata{
		Commit:  commit,
		RootDir: rootDir,
		Skip:    skip,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0644)
}

// ReadMetadata loads index metadata from metadata.json in dir.
func ReadMetadata(dir string) (*Metadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, err
	}

	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// CurrentCommit returns the current git HEAD commit hash in the given repository.
// Returns an error if the directory is not a git repository or git is unavailable.
func CurrentCommit(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
