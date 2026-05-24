// Package main is the CLI entrypoint for grepturbo.
//
// GrepTurbo accelerates regex searches by building a trigram-indexed inverted
// index over a codebase, then using that index to filter down to candidate
// files before running the actual regex engine. This reduces query time from
// seconds (scanning every file) to milliseconds (scanning only candidates).
//
// Architecture overview:
//
//	regex pattern
//	   ↓
//	decompose into required trigrams
//	   ↓
//	lookup trigrams in index (mmap'd hash table)
//	   ↓
//	intersect posting lists → candidate file IDs
//	   ↓
//	run regex engine on candidates only
//	   ↓
//	return matches (file:line:text format)
//
// The index is built once and persisted to disk in three files:
//   - lookup.idx: mmap'd fixed-size hash table (trigram → byte offset)
//   - postings.dat: posting lists (count + file IDs, read on demand)
//   - files.idx: file ID → file path mapping
//
// See docs/ARCHITECTURE.md for full diagrams and design decisions.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"grepturbo/internal/agentinit"
	"grepturbo/internal/index"
	"grepturbo/internal/query"
)

const defaultIndexDir = ".grepturbo"

func main() {
	// shorthand parsing: if the first arg isn't a known subcommand or flag,
	// treat it as a search pattern so `grepturbo <pattern>` works alongside
	// `grepturbo search <pattern>`. This improves usability without breaking
	// explicit subcommand calls.
	knownSubcmds := map[string]bool{"build": true, "search": true, "init": true, "help": true, "completion": true}
	if len(os.Args) > 1 {
		first := os.Args[1]
		if first != "" && first[0] != '-' && !knownSubcmds[first] {
			// Inject "search" before the pattern
			newArgs := make([]string, 0, len(os.Args)+1)
			newArgs = append(newArgs, os.Args[0], "search")
			newArgs = append(newArgs, os.Args[1:]...)
			os.Args = newArgs
		}
	}

	rootCmd := &cobra.Command{
		Use:   "grepturbo",
		Short: "Index-accelerated regex search",
		Long:  "grepturbo — build a trigram index over a codebase and query it with regex patterns.",
	}

	// build subcommand
	var buildRoot, buildOut string
	var buildSkip []string

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Walk a directory and build the search index",
		Long: `Build walks the specified directory tree, extracts trigrams from each file,
and serializes an inverted index to disk. The index enables O(log n) candidate
file filtering during queries.

The index is built once and reused for multiple searches. Incremental updates
via git commit tracking keep the index fresh across edits.`,
		Example: `  grepturbo build -root ./myproject -out .grepturbo
  grepturbo build -root . --skip node_modules --skip dist`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(buildRoot, buildOut, buildSkip)
		},
	}
	buildCmd.Flags().StringVarP(&buildRoot, "root", "r", ".", "root directory to index")
	buildCmd.Flags().StringVar(&buildOut, "out", defaultIndexDir, "directory to write the index")
	buildCmd.Flags().StringArrayVar(&buildSkip, "skip", nil, "directory name to skip (repeatable)")

	// search subcommand
	var searchIdx string

	searchCmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Query the index with a regex pattern",
		Long: `Search decomposes a regex pattern into required trigrams, looks them up
in the index, intersects the posting lists to find candidate files,
and runs the regex engine only on those candidates.

Output format: file:line:text (compatible with grep -n).
Exit status 1 if no matches are found (grep convention).`,
		Example: `  grepturbo search -index .grepturbo 'func.*Error'
  grepturbo search 'TODO'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(searchIdx, args[0])
		},
	}
	searchCmd.Flags().StringVarP(&searchIdx, "index", "i", defaultIndexDir, "index directory to query")

	// init subcommand — wire agent instructions into ~/.claude/
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Install agent instructions into ~/.claude/ so Claude Code uses grepturbo",
		Long: `Init writes ~/.claude/GrepTurbo.md and adds @GrepTurbo.md to ~/.claude/CLAUDE.md.
This configures Claude Code to prefer grepturbo search over grep/ripgrep
for regex searches across the codebase.

After running this, build the index once with 'grepturbo build' in your project.
Claude Code will then automatically use grepturbo for all pattern searches.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := agentinit.Setup()
			if err != nil {
				return err
			}
			fmt.Println(summary)
			fmt.Println("\nAgent instructions installed. Run 'grepturbo build' in your project, then Claude Code will use grepturbo for searches.")
			return nil
		},
	}

	rootCmd.AddCommand(buildCmd, searchCmd, initCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runBuild orchestrates the index build pipeline:
// 1. Walk the directory tree
// 2. Extract trigrams from each file
// 3. Build in-memory posting lists
// 4. Serialize to disk (lookup.idx, postings.dat, files.idx)
func runBuild(root, outDir string, skip []string) error {
	fmt.Fprintf(os.Stderr, "Building index for %s → %s\n", root, outDir)

	b := index.NewBuilder()
	if err := b.Build(root, skip...); err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Indexed %d files, %d unique trigrams\n",
		len(b.Files), len(b.Posts))

	if err := index.Write(b, outDir); err != nil {
		return fmt.Errorf("error writing index: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Done.\n")
	return nil
}

// runSearch executes a regex query using the indexed search pipeline:
// 1. Open the index reader (mmap lookup table)
// 2. Call query.Search to decompose pattern, intersect posting lists, run regex
// 3. Handle index drift (commit mismatch) by rebuilding if needed
// 4. Output matches in grep-compatible format
func runSearch(idxDir, pattern string) error {
	r, err := index.NewReader(idxDir)
	if err != nil {
		return fmt.Errorf("error opening index: %w", err)
	}
	defer r.Close()

	matches, err := query.Search(r, pattern)
	if err != nil {
		if drift, ok := err.(*query.ErrCommitDrift); ok {
			fmt.Fprintf(os.Stderr, "Notice: %s. Rebuilding...\n", drift.Error())
			r.Close() // close before rebuild
			if err := runBuild(r.Meta.RootDir, idxDir, r.Meta.Skip); err != nil {
				return err
			}
			// Re-open and try search again
			r2, err := index.NewReader(idxDir)
			if err != nil {
				return err
			}
			defer r2.Close()
			matches, err = query.Search(r2, pattern)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("error searching: %w", err)
		}
	}

	if len(matches) == 0 {
		os.Exit(1) // grep convention: exit 1 when no matches
	}

	for _, m := range matches {
		fmt.Printf("%s:%d:%s\n", m.File, m.Line, m.Text)
	}
	return nil
}
