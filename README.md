<div align="center">

<img 
  width="400" 
  height="300" 
  alt="GrepTurbo Logo" 
  src="https://github.com/user-attachments/assets/f820407e-ffa3-4434-b7fc-4f476a9e20c3"
/>

# GrepTurbo

*Index-accelerated regex search. Skip irrelevant files entirely.*

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)
[![Build](https://img.shields.io/badge/Build-Passing-brightgreen?style=flat)]()
[![Speedup](https://img.shields.io/badge/Speedup-6--7x_faster-orange?style=flat)]()
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/yanurag-dev/fastregex)

</div>

---

> **GrepTurbo** builds a local trigram index over your codebase so regex queries skip irrelevant files entirely — instead of scanning every byte like `grep`. The bigger your codebase, the bigger the win.

---

## Benchmark

Tested on the Go standard library source (~10,000 files):

<div align="center">

| Tool | Time | Files Scanned |
|---|---|---|
| `grep -rn` | 2.4 – 3.1s | All 10,000 |
| `GrepTurbo search` | 0.4 – 0.9s | ~50 candidates |

</div>

**6–7x faster** on 10k files. Grows with codebase size. Repeated queries get faster as the OS caches the mmap'd index in the page cache.

---

## Install

### One-liner (Recommended)

```bash
curl -fsSL https://yanurag-dev.github.io/GrepTurbo/install.sh | bash
```

Installs the latest release to `/usr/local/bin/grepturbo`. Override the install directory:

```bash
INSTALL_DIR=~/.local/bin curl -fsSL https://yanurag-dev.github.io/GrepTurbo/install.sh | bash
```

### Pre-compiled Binaries

Download the latest binary for your platform from the [Releases](https://github.com/yanurag-dev/GrepTurbo/releases) page.

### From Source

```bash
git clone https://github.com/yanurag-dev/GrepTurbo
cd GrepTurbo
go build -o grepturbo ./cmd/grepturbo
```

---

## Usage

**Step 1 — build the index** (once, or when files change):

```bash
GrepTurbo build -root ./myproject -out .GrepTurbo
```

**Step 2 — search:**

```bash
GrepTurbo search -index .GrepTurbo 'func.*Error'
```

Output is `file:line:text`, same as `grep -n`:

```
internal/index/reader.go:25:func NewReader(dir string) (*Reader, error) {
internal/query/search.go:26:func Search(r *index.Reader, pattern string) ([]Match, error) {
```

### Flags

```
GrepTurbo build
  -root   <dir>    Directory to index (default: .)
  -out    <dir>    Where to write the index (default: .GrepTurbo)

GrepTurbo search
  -index  <dir>    Index directory to query (default: .GrepTurbo)
```

---

## How It Works

```
regex → trigram decomposition → index lookup → intersect posting lists → candidate files → verify with regex
```

1. **Trigram decomposition** — `func.*Error` contains literals `func` and `Error`, producing trigrams `fun unc` and `Err rro ror`
2. **Index lookup** — each trigram maps to a sorted posting list of file IDs that contain it
3. **Intersection** — only files containing *all* required trigrams become candidates (10,000 → ~50)
4. **Verification** — the real regex engine runs only on those ~50 files

**The golden invariant:** if a file matches the regex, it will always appear in the candidate set. No false negatives, ever.

### Index on Disk

```
.GrepTurbo/
  lookup.idx    mmap'd hash table — trigram → byte offset in postings.dat
  postings.dat  posting lists — [count][fileID, fileID, ...]
  files.idx     fileID → filepath mapping
```

Only `lookup.idx` is loaded into memory (mmap'd). Posting lists are read from disk on demand.

---

## Testing

Run the dynamic test script to benchmark and verify correctness against `grep`:

```bash
# Test default patterns on this repo
./scripts/test.sh

# Test a single pattern
./scripts/test.sh 'func.*Error'

# Test on any large codebase
./scripts/test.sh 'func.*Error' /path/to/large/repo
```

The script builds the binary, indexes the target directory, runs each pattern through both `grep` and `GrepTurbo`, compares results, and reports speedup + any false negatives.

```bash
# Run unit + integration tests
go test ./...

# Run a specific test
go test ./internal/query/... -run TestCorrectnessVsGrep -v

# Run benchmarks
go test ./... -bench=.
```

---

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for full diagrams covering the index build pipeline, query flow, on-disk format, regex decomposition rules, and incremental sync strategy.

---

<div align="center">

Built with Go · MIT License<br>
[![Coverage](https://img.shields.io/badge/coverage-90%25-brightgreen?style=flat)](https://your-coverage-service-url)

</div>
