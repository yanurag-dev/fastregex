# Fast Regex Search: Indexing Text for Agent Tools

**Author:** Vicent Marti | **Date:** March 23, 2026 | **Read time:** 21 minutes

**Source:** https://cursor.com/blog/fast-regex-search

---

## Overview

Cursor is implementing local regex search indexes to dramatically accelerate how AI agents find text in large codebases. The approach addresses a critical bottleneck: `ripgrep` queries routinely exceed 15 seconds on massive monorepos, stalling agentic workflows.

---

## The Problem

Traditional tools like `ripgrep` must scan entire file systems to match regular expressions. While exceptionally fast at pattern matching, they cannot skip irrelevant files. For Enterprise customers managing sprawling codebases, this creates unacceptable latency when agents repeatedly invoke search operations.

---

## Historical Context

The concept of indexing for regex performance dates to 1993 research by Zobel, Moffat, and Sacks-Davis on compressed inverted files. Russ Cox popularized the approach in 2012 following Google Code Search's shutdown.

---

## Core Indexing Approaches

### 1. Inverted Indexes & Trigrams

Traditional search engines tokenize documents into words. However, source code requires different tokenization. The classic algorithm uses **trigrams** — overlapping three-character sequences — as index keys.

**Why trigrams?**
- Bigrams create massive posting lists
- Quadgrams create billions of index keys
- Trigrams strike the optimal balance between specificity and manageability

**How it works:**
- Text is broken into overlapping 3-character sequences
- Each trigram maps to a posting list (list of files containing it)
- For a regex query: decompose into required trigrams → intersect posting lists → verify matches in candidate files

---

### 2. Suffix Arrays

Nelson Elhage's `livegrep` implementation (2015) uses sorted suffix arrays instead of inverted indexes.

- Binary searches find pattern positions efficiently
- Requires concatenating entire codebases into single strings
- Makes dynamic updates expensive and scaling difficult

---

### 3. Probabilistic Masks (GitHub's Project Blackbird)

Augments trigram posting lists with **bloom filters** encoding:

- **nextMask:** Characters following each trigram
- **locMask:** Position information (mod 8)

This effectively enables quadgram-level queries while maintaining trigram storage efficiency.

**Tradeoff:** Bloom filters saturate as data accumulates, degrading performance.

---

### 4. Sparse N-grams *(most sophisticated)*

Currently used by ClickHouse and GitHub's Code Search. Extracts variable-length n-grams deterministically based on character pair weights.

**Algorithm:**
1. Assign weights to character pairs using hash functions (initially CRC32, improved via character pair frequency analysis)
2. Extract n-grams where boundary weights exceed all interior weights
3. At query time, use a **"covering algorithm"** generating only the minimal set of essential n-grams

**Key innovation:** Frequency-based weighting prioritizes rare character pairs, minimizing index lookups and candidate documents. Smaller index, fewer candidates to verify.

---

## Cursor's Implementation

### Client-Side Processing

Unlike server-based competitors, Cursor builds and queries indexes **locally**. Benefits:

- **Privacy:** No codebase transmission to servers
- **Latency:** Eliminates network roundtrips during agent operations
- **Freshness:** Indexes reflect agent writes immediately
- **Efficiency:** File scanning occurs locally after index narrows candidates

### Index Storage Strategy

Two-file architecture optimizes memory usage:

1. **Postings file:** Sequential document lists flushed during construction
2. **Lookup table:** Hash-to-offset mappings, memory-mapped for fast binary search

Only the lookup table loads into memory; postings are read directly from disk at specific offsets.

**Hash collisions** are acceptable — they only widen the candidate set, never produce incorrect results.

### Synchronization & Updates

Index state anchors to **Git commits**. User and agent modifications layer above the committed baseline, enabling:
- Rapid startup synchronization
- Quick incremental updates
- Deterministic state management

---

## Performance Impact

Testing demonstrates substantial improvements on large repositories:

| Scenario | Improvement |
|---|---|
| Investigation / debugging | 30–40% time reduction |
| Refactoring operations | 25–35% improvement |
| Agent iteration | More effective with instant results |

---

## Future Directions

Cursor plans to continue optimizing semantic indexes alongside regex indexing, exploring complementary context-gathering techniques for agentic development workflows.

---

## Implementation Notes (for building this)

### Trigram Decomposition of Regex Patterns

- **Literal strings** → extract all overlapping trigrams
- **Alternations** → separate branches with unioned posting lists
- **Character classes** → generate trigrams for each element in the class

### Two Key Data Structures

```
Lookup Table (mmap'd in memory):
  n-gram hash → offset in postings file

Postings File (on disk):
  offset → [file1, file2, file3, ...]
```

### Query Flow

```
regex query
  → extract required n-grams
  → look up each n-gram in lookup table
  → get posting lists from disk
  → intersect posting lists → candidate files
  → run ripgrep/regex engine on candidate files only
  → return matches
```
