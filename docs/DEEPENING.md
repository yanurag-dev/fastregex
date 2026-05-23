# Deepening Opportunities

This document tracks identified architectural friction and proposed refactors to improve **depth**, **leverage**, and **locality** in GrepTurbo.

## 1. Unified Index Module
- **Files**: `internal/index/reader.go`, `internal/index/sync.go`, `internal/query/search.go`
- **Problem**: The `Search` function is **shallow** because it manually orchestrates **Baseline** lookup, **Overlay** lookup, and **Tombstone** filtering. This leaks internal index layering into the query engine.
- **Solution**: Deepen the `internal/index` module by introducing a unified interface that provides a single `Candidates(trigrams []uint32) ([]string, error)` method.
- **Benefits**:
    - **Locality**: Concentrates knowledge of multi-layer merging in one place.
    - **Leverage**: Simplifies the query engine by hiding sync and filtering complexity.
    - **Testability**: Enables testing candidate selection logic in isolation.

## 2. Search Pipeline Module
- **Files**: `internal/query/search.go`, `internal/query/decompose.go`
- **Problem**: `Search` is a procedural, monolithic function mixing regex decomposition, index coordination, and file I/O.
- **Solution**: Create a **Deep** search module that encapsulates the pipeline, using `QueryableIndex` and a `Matcher` as **Adapters**.
- **Benefits**:
    - **Locality**: Isolates the "rules" of the search process.
    - **Leverage**: Provides a simple `Execute(pattern)` interface.

## 3. Overlay as LiveIndex
- **Files**: `internal/index/sync.go`
- **Problem**: `Sync()` returns a **shallow** "bag of data" (`Overlay` struct) that callers must know how to interpret.
- **Solution**: Turn `Overlay` into a `LiveIndex` module that satisfies the same interface as the **Baseline** index, allowing for polymorphic treatment.
- **Benefits**:
    - **Depth**: Leverages polymorphism to simplify the merging logic.
    - **Locality**: Isolates dirty-file indexing from query logic.
