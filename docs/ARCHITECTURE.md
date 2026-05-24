# GrepTurbo — Architecture

## Overview

GrepTurbo builds a local trigram-indexed inverted index over a codebase so that regex queries
can skip irrelevant files entirely, instead of scanning every file like `ripgrep` does.

The index answers the question:
> "Which files *might* contain a match for this regex?"

Then the regex engine runs only on that small candidate set.

**Key win:** On a 10,000-file codebase, queries drop from ~2.5s (scanning all) to ~0.5s (50 candidates). The speedup grows with codebase size.

---

## High-Level Flow

### Build Phase (one-time or incremental)

```
User runs: grepturbo build -root ./myproject

           ↓

   Walk directory tree, read files
   Extract trigrams from each file
   Build in-memory posting lists
   
           ↓
   
   Serialize to disk:
   - lookup.idx    (mmap'd hash table: trigram → offset)
   - postings.dat  (posting lists: count + file IDs)
   - files.idx     (file ID → path mapping)
   - metadata.json (root dir, skip patterns, git commit)
```

### Query Phase (repeated, fast)

```mermaid
flowchart TD
    A[User runs: grepturbo search 'pattern'] --> B[Query Engine]
    B --> C[Regex Decomposer]
    C --> D["Extract required trigrams\ne.g. foobar → foo, oob, oba, bar"]
    D --> E[Index Reader]
    E --> F[Lookup Table\nlookup.idx - mmap'd in memory]
    F --> G[Postings File\npostings.dat - read from disk]
    G --> H[Intersect posting lists\n→ candidate file IDs]
    H --> I[File Map\nfiles.idx\nID → path]
    I --> J[Run regex on candidate files only]
    J --> K[Return matches to user]
```

### Freshness Check

```
When search runs:
  - Read git HEAD commit from .git/
  - Check if index metadata.json matches this commit
  - If no match → full rebuild + retry search
  - If match → check git status for uncommitted files
  - Re-index only dirty files, patch posting lists in memory
```

---

## Component Architecture

```mermaid
flowchart LR
    subgraph cmd
        CLI[main.go\nCLI entrypoint]
    end

    subgraph internal-trigram
        TRI[trigram.go\nT = uint32\nExtract, FromBytes]
    end

    subgraph internal-posting
        POST[posting.go\nList = map trigram to fileIDs\nAdd, Get, Intersect]
    end

    subgraph internal-index
        BUILDER[builder.go\nWalk files\nExtract trigrams\nBuild posting lists]
        WRITER[writer.go\nSerialize to disk\nlookup + postings + files]
        READER[reader.go\nmmap lookup table\nRead postings on demand]
    end

    subgraph internal-query
        DECOMP[decompose.go\nRegex to required trigrams\nLiterals, alternations, char classes]
        SEARCH[search.go\nLookup trigrams, intersect, candidates\nCall regex engine on candidates]
    end

    subgraph internal-sync
        SYNC[sync.go\nGit commit baseline\nTrack dirty files\nIncremental updates]
    end

    CLI --> SEARCH
    CLI --> BUILDER
    SEARCH --> DECOMP
    SEARCH --> READER
    READER --> POST
    BUILDER --> TRI
    BUILDER --> POST
    BUILDER --> WRITER
    SYNC --> BUILDER
```

---

## Data Flow: Index Build

```mermaid
sequenceDiagram
    participant Builder
    participant Trigram
    participant Posting
    participant Writer

    Builder->>Builder: Walk all files in repo
    loop Each file
        Builder->>Trigram: Extract(fileContent)
        Trigram-->>Builder: []T (trigrams)
        loop Each trigram
            Builder->>Posting: Add(trigram, fileID)
        end
    end
    Builder->>Writer: Serialize(postingList, fileMap)
    Writer->>Writer: Write postings.dat (sequential)
    Writer->>Writer: Write lookup.idx (hash → offset)
    Writer->>Writer: Write files.idx (fileID → path)
```

---

## Data Flow: Query

```mermaid
sequenceDiagram
    participant User
    participant QueryEngine
    participant Decomposer
    participant Reader
    participant RegexEngine

    User->>QueryEngine: Search("func.*Error")
    QueryEngine->>Decomposer: ExtractTrigrams("func.*Error")
    Note over Decomposer: Literals only: fun, unc from func\n.* wildcard yields no trigrams\nErr, rro, ror from Error
    Decomposer-->>QueryEngine: [fun, unc, Err, rro, ror]
    loop Each trigram
        QueryEngine->>Reader: Lookup(trigram)
        Reader-->>QueryEngine: []fileID
    end
    QueryEngine->>QueryEngine: Intersect all posting lists
    QueryEngine-->>RegexEngine: candidate file paths
    RegexEngine->>RegexEngine: Run actual regex on each file
    RegexEngine-->>User: matches with line numbers
```

---

## On-Disk Format

```mermaid
flowchart TD
    subgraph lookup-idx
        LK["Fixed-size hash table\nslot 0: hash=0x00, offset=0\nslot 1: hash=0xAB, offset=104\nslot 2: hash=0x00, offset=0 empty\nEach slot: 8 bytes, 4 hash + 4 offset"]
    end

    subgraph postings-dat
        PD["Variable-length records\noffset 0: count=3, fileID 1, 4, 9\noffset 104: count=1, fileID 2"]
    end

    subgraph files-idx
        FI["Sequential file paths\nfileID 1 → src/main.go\nfileID 2 → src/server.go\nfileID 4 → pkg/util.go"]
    end

    lookup-idx -- "offset" --> postings-dat
    postings-dat -- "fileID" --> files-idx
```

---

## Regex Decomposition Rules

```mermaid
flowchart TD
    R[Regex Pattern] --> L{Has literal\nsubstrings?}
    L -- yes --> LT[Extract trigrams\nfrom each literal]
    L -- no --> NONE[No trigrams\nScan all files]

    LT --> ALT{Alternation?\ne.g. foo or bar}
    ALT -- yes --> UNION[Union of trigrams\nfrom each branch]
    ALT -- no --> CC{Character class?\ne.g. a-z}
    CC -- yes --> CCT[Generate trigrams\nfor each element]
    CC -- no --> DONE[Final trigram set]
    UNION --> DONE
    CCT --> DONE

    DONE --> INT[Intersect posting lists\nfor all trigrams]
    INT --> CAND[Candidate files]
```

---

## Incremental Sync Strategy

```mermaid
flowchart TD
    START[fastregex starts] --> GIT[Read current Git HEAD commit]
    GIT --> CACHED{Index matches\nthis commit?}
    CACHED -- yes --> DIRTY[Check for uncommitted\ndirty files via git status]
    CACHED -- no --> FULL[Full rebuild from HEAD]
    DIRTY --> PATCH[Re-index only dirty files\nPatch posting lists in memory]
    PATCH --> READY[Index ready]
    FULL --> READY
```

---

## Build Order (implementation sequence)

```mermaid
flowchart LR
    P1["Phase 1: trigram.go\nExtract overlapping trigrams\nfrom strings"] -->
    P2["Phase 2: posting.go\nBuild in-memory\ninverted index"]
    P2 -->
    P3["Phase 3: index/builder.go\nWalk files, extract trigrams,\nbuild posting lists"]
    P3 -->
    P4["Phase 3b: index/writer.go\nSerialize to disk\n3 files: lookup, postings, files"]
    P4 -->
    P5["Phase 4: index/reader.go\nmmap lookup table,\nread postings on demand"]
    P5 -->
    P6["Phase 5: query/decompose.go\nRegex → required trigrams"]
    P6 -->
    P7["Phase 6: query/search.go\nFull pipeline:\nlookup → intersect → regex"]
    P7 -->
    P8["Phase 7: index/sync.go\nGit tracking + dirty overlay\nfor incremental updates"]
    P8 -->
    P9["Phase 8: cmd/main.go\nCLI: build, search, init"]
```

**Status:** Phases 1–9 complete. All core functionality implemented.

---

## Map to Disk: The Translation Layer

### Problem: In-Memory Maps Can't Go Directly to Disk

A Go `map[uint32][]uint32` is a heap-allocated structure with pointer chains:

```go
m := make(map[uint32][]uint32)
m[0x12345678] = []uint32{1, 5, 9}
```

This creates:
- hmap struct with metadata
- Bucket array with pointers
- Key-value pairs in buckets
- Overflow chains for collisions

**This only works in RAM.** You cannot serialize it to disk and read it back as a map. What you get is raw bytes - no structure, no pointers.

### Solution: Convert Map to Disk Format

The architecture solves this with a three-phase approach:

```
┌──────────────────────────────────────────────────────────────────┐
│                        BUILD TIME                                │
│                                                                  │
│   builder.go              writer.go                              │
│   ┌──────────────┐       ┌──────────────────────────────────┐   │
│   │ map[uint32]   │  ──►  │ lookup.idx: fixed-size hash table│   │
│   │ []uint32      │       │ postings.dat: sequential records │   │
│   │ (in-memory)   │       │ files.idx: fileID → path         │   │
│   └──────────────┘       └──────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│                        QUERY TIME                                │
│                                                                  │
│   reader.go                                                       │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ 1. mmap lookup.idx (just bytes, no map!)                  │   │
│   │ 2. Binary search for hash                                  │   │
│   │ 3. Get offset → read postings.dat at that offset          │   │
│   │ 4. Return posting list                                     │   │
│   └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

### Writer Serialization Process

```go
// Simplified writer.go logic
func WriteIndex(postingList map[uint32][]uint32) {
    // Step 1: Sort hashes for binary search
    sortedHashes := make([]uint32, 0, len(postingList))
    for hash := range postingList {
        sortedHashes = append(sortedHashes, hash)
    }
    sort.Slice(sortedHashes, func(i, j int) bool {
        return sortedHashes[i] < sortedHashes[j]
    })

    // Step 2: Write postings sequentially to postings.dat
    offsets := make([]uint32, len(sortedHashes))
    for i, hash := range sortedHashes {
        fileIDs := postingList[hash]
        offset := writePostings(fileIDs)  // returns file offset
        offsets[i] = offset
    }

    // Step 3: Write lookup table (hash → offset)
    // Fixed-size: 8 bytes per entry (4 bytes hash + 4 bytes offset)
    for i, hash := range sortedHashes {
        writeUint32(hash)
        writeUint32(offsets[i])
    }
}
```

### Reader Lookup Process

```go
// Simplified reader.go logic
func (r *Reader) Lookup(hash uint32) []uint32 {
    // Step 1: Binary search mmap'd lookup table
    offset := binarySearch(r.lookupTable, hash)
    if offset == 0 {
        return nil  // not found
    }

    // Step 2: Seek to offset in postings.dat
    r.postingsFile.Seek(offset, os.SEEK_SET)

    // Step 3: Read count + fileIDs
    count := readUint32()
    fileIDs := make([]uint32, count)
    for i := uint32(0); i < count; i++ {
        fileIDs[i] = readUint32()
    }
    return fileIDs
}
```

### Why This Works

| In-Memory Map | On-Disk Format |
|---------------|----------------|
| `m[key]` | Binary search on sorted array |
| Dynamic resize | Fixed size (pre-allocated at build) |
| Pointer chains | Just offsets (no pointers) |
| O(1) amortized | O(log n) search + O(1) read |
| GC managed | mmap'd - no GC, OS manages pages |

### Key Insight

> **The map exists only during BUILD time.** After writing to disk, it's gone.
> At query time, we reconstruct the lookup logic using the on-disk format.

This is why we have separate components:
- `builder.go` - builds in-memory map
- `writer.go` - converts map to disk format
- `reader.go` - queries disk format without needing a map

---

## Key Invariant: No False Negatives

**The golden rule:**
> If a file contains a regex match, it **must** appear in the candidate set.
> False positives (candidates that don't match) are acceptable and expected.

This is enforced by:
- **Conservative trigram decomposition**: If a pattern contains wildcards (e.g., `func.*Error`), 
  extract only trigrams from literals. If a literal substring exists, any file matching the regex 
  must contain all trigrams from that substring.
- **Conservative intersection**: Return the union of all candidates, never a subset that might 
  exclude files with partial matches.
- **Hash collisions allowed**: If two different trigrams hash to the same value, 
  the candidate set widens—but matches are never lost.

**Verification:** See `internal/query/search_integration_test.go` for correctness tests against `grep` output.

---

## CLI Integration (cmd/grepturbo/main.go)

GrepTurbo exposes three subcommands:

### `grepturbo build [flags]`

Walks a directory tree and builds the index. Stores three files (lookup.idx, postings.dat, files.idx)
plus metadata.json recording root dir, skip patterns, and git commit hash.

**Flags:**
- `-root <dir>`: Directory to index (default: `.`)
- `-out <dir>`: Directory to write the index (default: `.grepturbo`)
- `--skip <dir>`: Directory name to skip (repeatable; e.g., `--skip node_modules --skip dist`)

**Example:**
```bash
grepturbo build -root ./myproject -out .grepturbo --skip node_modules --skip .git
```

### `grepturbo search <pattern> [flags]`

Queries the index with a regex pattern. Automatically detects index drift and rebuilds if needed.
Output is `file:line:text` (grep-compatible).

**Flags:**
- `-index <dir>`: Index directory to query (default: `.grepturbo`)

**Examples:**
```bash
grepturbo search 'func.*Error'
grepturbo search -index .grepturbo 'TODO.*FIXME'
grepturbo 'TODO'  # shorthand: search is implicit
```

**Exit codes:**
- `0`: Matches found and printed to stdout
- `1`: No matches found (grep convention)
- `2`: Error (invalid regex, missing index, etc.)

### `grepturbo init`

Installs agent instructions into `~/.claude/` so Claude Code prefers `grepturbo search` over `grep/ripgrep`.
After running `init`, Claude Code will use grepturbo for all pattern searches in the project.

---

## Key Design Decisions

| Decision | Choice | Why |
|---|---|---|
| Trigram size | 3 chars | Bigrams = too many collisions, quadgrams = too many keys |
| Packing | `uint32` | O(1) map ops vs O(n) string comparison |
| Lookup table | Hash table, mmap'd | Stays in OS page cache, no heap allocation |
| Postings | Sequential on disk | Read only the lists you need, not the whole file |
| Hash collisions | Allowed | Widen candidate set (false positives ok), never miss matches |
| False negatives | Never allowed | The golden invariant: if a file matches, it must be a candidate |
| Index freshness | Git commit + dirty overlay | Fast startup, correct for agent workflows |
