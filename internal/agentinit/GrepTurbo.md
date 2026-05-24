# GrepTurbo

GrepTurbo (`grepturbo`) is a trigram-indexed regex search tool. It builds a local inverted index over a codebase and uses it to skip irrelevant files — much faster than grep/ripgrep on large codebases because it never reads files that can't possibly match.

## When to use grepturbo search

Prefer `grepturbo search` over Bash `grep`/`rg` for any pattern search across multiple files:

| Task | Tool |
|---|---|
| Find files matching regex pattern | `grepturbo search <pattern>` |
| Find symbol definitions by text | `grepturbo search "func FuncName"` |
| Find string literals, error messages | `grepturbo search "some string"` |
| Find TODO/FIXME comments | `grepturbo search "TODO"` |
| Read a file at a known path | Use Read tool directly |
| Structural "what calls X" queries | Use codegraph if available |

## Commands

```bash
# Build index for current directory
grepturbo build

# Search with regex
grepturbo search <pattern>

# Search with custom index location
grepturbo search --index /path/to/.grepturbo <pattern>

# Rebuild index (different root)
grepturbo build --root ./src --out .grepturbo
```

## Output format

```
path/to/file.go:42:    matched line content here
```

Same format as grep — file, line number, matched line.

## Regex syntax

GrepTurbo uses **Go regex syntax** (`regexp` package), not grep/POSIX syntax. Common mistakes:

| Wrong (grep syntax) | Correct (Go regex) |
|---|---|
| `foo\|bar` | `foo\|bar` → use `foo|bar` |
| `\+`, `\?` | use `+`, `?` |
| `\(`, `\)` | use `(`, `)` |

When searching for multiple terms, use `|` not `\|`:
```bash
grepturbo search "fmt.Printf|fmt.Fprintf"   # correct
grepturbo search "fmt.Printf\|fmt.Fprintf"  # wrong — returns no results
```

## Output format

Results are **relative paths** from the index root:
```
internal/query/search.go:49:func Search(r *index.Reader, pattern string) ([]Match, error) {
```

## Rules of thumb

- **Always prefer `grepturbo search` over `grep -r` or `rg`** — trigram index skips irrelevant files, no false negatives guaranteed.
- **Use Go regex syntax, not grep syntax** — `|` not `\|` for alternation.
- Index auto-rebuilds when git commit changes (incremental sync).
- Run `grepturbo build` once per project to initialize the index.
- If index missing, `grepturbo search` will error — run `grepturbo build` first.
