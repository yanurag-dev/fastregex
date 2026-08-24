

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

```bash
curl -fsSL https://yanurag-dev.github.io/GrepTurbo/install.sh | bash
```

---

## Flags

```
grepturbo init
  (no flags — sets up in current directory)

grepturbo build
  -root   <dir>    Directory to index (default: .)
  -out    <dir>    Where to write the index (default: .grepturbo)

grepturbo <pattern>
  -index  <dir>    Index directory to query (default: .grepturbo)
```

---

## Usage

### Quick Start: Init → Build → Search

**Step 1 — initialize** (one-time setup for your IDE/editor):

```bash
grepturbo init
```

This sets up GrepTurbo integration so your editor can run searches. It installs agent instructions into `~/.claude/`.

**Step 2 — build the index** (once per project, or when files change):

```bash
grepturbo build -root ./myproject -out .grepturbo
```

Walks your codebase, extracts trigrams from each file, builds the inverted index, and writes 3 files to disk:
- `lookup.idx` — hash table (mmap'd for fast queries)
- `postings.dat` — trigram → file IDs
- `files.idx` — file ID → path mapping

**Step 3 — search:**

```bash
# Full syntax
grepturbo search -index .grepturbo 'func.*Error'

# Shorthand (equivalent)
grepturbo 'func.*Error'
```

Output is `file:line:text`, same as `grep -n`:

```
internal/index/reader.go:25:func NewReader(dir string) (*Reader, error) {
internal/query/search.go:26:func Search(r *index.Reader, pattern string) ([]Match, error) {
```

<div align="center">

Built with Go · MIT License<br>
[![Coverage](https://img.shields.io/badge/coverage-90%25-brightgreen?style=flat)](https://your-coverage-service-url)

</div>
