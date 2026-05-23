# GrepTurbo Context

GrepTurbo provides index-accelerated regex search that stays in sync with real-time code changes using Git.

## Language

**Baseline**:
The immutable on-disk index tied to a specific Git commit.
_Avoid_: Old index, Disk index, Saved index

**Overlay**:
The transient in-memory index of uncommitted (modified, added, or untracked) files detected during search.
_Avoid_: Live index, Delta index, Fresh index

**Commit Drift**:
The state where the current Git HEAD differs from the commit hash stored in the **Baseline**.

**Tombstone**:
A list of file paths that have been deleted since the **Baseline** was built, used to filter out dead results.

## Relationships

- A **Search** merges results from the **Baseline** and the **Overlay**, while filtering out paths in the **Tombstone** set.
- **Commit Drift** triggers an automatic rebuild of the **Baseline**.

## Example dialogue

> **Dev:** "If I just edited a file but haven't committed it, will the search find it?"
> **Domain expert:** "Yes, the **Overlay** will index your unsaved changes and merge them with the **Baseline** results."
