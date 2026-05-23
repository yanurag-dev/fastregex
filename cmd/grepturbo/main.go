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
		Long: `Writes ~/.claude/GrepTurbo.md and adds @GrepTurbo.md to ~/.claude/CLAUDE.md.

After running this, Claude Code will prefer grepturbo search over grep/ripgrep
for regex searches across the codebase.`,
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
