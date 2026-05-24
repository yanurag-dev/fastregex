// Package posting implements an inverted index mapping trigrams to file IDs.
//
// The index is built in two phases: AddBatch appends fileIDs for each trigram,
// then Finalize sorts and deduplicates all posting lists. Once finalized,
// Get retrieves a posting list and Intersect finds files matching multiple trigrams.
//
// The key invariant: all posting lists remain sorted by file ID. This enables
// efficient set operations and ensures no false negatives during query execution.
package posting

import (
	"sort"

	"grepturbo/internal/trigram"
)

// List maps each trigram to a sorted slice of file IDs that contain it.
// Each posting list is maintained in sorted order (see Finalize and AddBatch).
type List map[trigram.T][]uint32

// AddBatch appends fileIDs to the posting list for trigram t without sorting.
// Call Finalize after all AddBatch calls to sort and deduplicate all lists.
func (l List) AddBatch(t trigram.T, fileIDs []uint32) {
	existing := l[t]
	l[t] = append(existing, fileIDs...)
}

// Finalize sorts and deduplicates all posting lists in-place.
//
// Each posting list is sorted by file ID, then consecutive duplicates are removed.
// After Finalize, all invariants are satisfied: lists are sorted and contain no duplicates.
// Call Finalize once after all AddBatch operations are complete.
func (l List) Finalize() {
	for t, ids := range l {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out := ids[:0]
		for _, id := range ids {
			if len(out) == 0 || out[len(out)-1] != id {
				out = append(out, id)
			}
		}
		l[t] = out
	}
}

// Get returns the sorted posting list (file IDs) for trigram t, or nil if t is not in the index.
func (l List) Get(t trigram.T) []uint32 {
	return l[t]
}

// Intersect returns the set of file IDs present in all provided sorted lists.
//
// All input lists must be sorted by file ID (typically from Get or previous Intersect calls).
// The algorithm sorts input lists by length and performs two-pointer merges for O(n) complexity per pair.
// Returns nil if any input is empty or no files match all trigrams.
func Intersect(lists ...[]uint32) []uint32 {
	if len(lists) == 0 {
		return nil
	}
	if len(lists) == 1 {
		out := make([]uint32, len(lists[0]))
		copy(out, lists[0])
		return out
	}

	// Start with the shortest list to minimise work
	sort.Slice(lists, func(i, j int) bool {
		return len(lists[i]) < len(lists[j])
	})

	result := lists[0]
	for _, next := range lists[1:] {
		result = intersectTwo(result, next)
		if len(result) == 0 {
			return nil
		}
	}
	return result
}

// intersectTwo computes the intersection of two sorted lists using a two-pointer merge.
// Both input lists must be sorted. Time complexity is O(len(a) + len(b)).
func intersectTwo(a, b []uint32) []uint32 {
	out := make([]uint32, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}
