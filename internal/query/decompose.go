// Package query implements regex pattern decomposition and full-pipeline search execution.
//
// The search pipeline has four stages:
//
// 1. Decompose: Parse the regex pattern and extract required trigrams (3-char sequences).
// If a file contains a match, it must contain all required trigrams. Patterns with
// wildcards or character classes cannot be decomposed and fall back to scanning all files.
//
// 2. Intersect: Query the index posting lists (baseline + git overlay) for files
// that contain all required trigrams. This produces a set of candidate files that
// might match the pattern.
//
// 3. Validate: Compile the regex and run it line-by-line against each candidate file.
// This filters false positives from the trigram-based filtering step.
//
// 4. Sync: Before each search, a git-based sync ensures the index includes recently
// modified and untracked files (overlay), accounts for deleted files (tombstones),
// and detects if the baseline index is stale (commit drift).
//
// The pipeline ensures no false negatives: if a file matches the regex, it will be
// in the final results. False positives from trigram filtering are eliminated by
// running the real regex on candidates.
package query

import (
	"regexp/syntax"

	"grepturbo/internal/trigram"
)

// Result holds the trigrams extracted from a regex pattern.
// If Wildcard is true, no trigrams could be extracted and the query
// must fall back to scanning every file.
type Result struct {
	Trigrams []trigram.T
	Wildcard bool
}

// Decompose parses a regex pattern and extracts trigrams that must appear in any matching file.
// If no useful trigrams can be extracted, it returns Wildcard=true.
//
// Trigram extraction rules:
//   - Literals: all overlapping 3-char sequences (e.g., "func" → ["fun", "unc"])
//   - Concatenation: union of trigrams from all sub-expressions (all parts must match)
//   - Alternation (|): union across branches; if any branch is a wildcard, result is a wildcard
//   - Wildcards (., .*, .+, [a-z], etc.): cannot require specific trigrams
//
// The returned Trigrams list filters files before running the regex engine.
// Wildcard=true means the regex cannot be filtered (scan all files).
func Decompose(pattern string) (*Result, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	re = re.Simplify()
	return extract(re), nil
}

// extract recursively walks the regex AST and returns the trigram Result
// for the subtree rooted at re.
func extract(re *syntax.Regexp) *Result {
	switch re.Op {

	case syntax.OpLiteral:
		// A literal string like "func" or "Error".
		// Convert runes to a string and extract trigrams.
		s := string(re.Rune)
		ts := trigram.Extract(s)
		if len(ts) == 0 {
			// Literal shorter than 3 chars — not useful for filtering
			return &Result{Wildcard: true}
		}
		return &Result{Trigrams: ts}

	case syntax.OpConcat:
		// A sequence: each sub-expression must all match in order.
		// Collect trigrams from ALL sub-expressions (intersect: file must
		// satisfy every part). If any part is a wildcard, we still keep
		// trigrams from the non-wildcard parts.
		result := &Result{}
		for _, sub := range re.Sub {
			r := extract(sub)
			if !r.Wildcard {
				result.Trigrams = append(result.Trigrams, r.Trigrams...)
			}
		}
		// If we got no trigrams at all from any sub-expression, it's a wildcard
		if len(result.Trigrams) == 0 {
			result.Wildcard = true
		}
		return result

	case syntax.OpAlternate:
		// foo|bar — file must match at least one branch.
		// We can only filter on trigrams that appear in EVERY branch.
		// If any branch is a wildcard, the whole alternation is a wildcard
		// (a file that doesn't contain "foo"'s trigrams might still match via "bar").
		var all [][]trigram.T
		for _, sub := range re.Sub {
			r := extract(sub)
			if r.Wildcard {
				return &Result{Wildcard: true}
			}
			all = append(all, r.Trigrams)
		}
		// Union: include trigrams from all branches.
		// (A file is a candidate if it might match any branch.)
		seen := make(map[trigram.T]struct{})
		var union []trigram.T
		for _, ts := range all {
			for _, t := range ts {
				if _, ok := seen[t]; !ok {
					seen[t] = struct{}{}
					union = append(union, t)
				}
			}
		}
		if len(union) == 0 {
			return &Result{Wildcard: true}
		}
		return &Result{Trigrams: union}

	case syntax.OpCapture:
		// Capturing group — transparent, just recurse into the single child
		if len(re.Sub) == 1 {
			return extract(re.Sub[0])
		}
		return &Result{Wildcard: true}

	case syntax.OpRepeat:
		// {n,m} repetition — recurse into the repeated sub-expression
		if len(re.Sub) == 1 && re.Min >= 1 {
			return extract(re.Sub[0])
		}
		return &Result{Wildcard: true}

	default:
		// OpStar, OpPlus, OpQuest, OpAnyChar, OpAnyCharNotNL,
		// OpCharClass, OpBeginText, OpEndText, etc.
		// None of these let us require specific trigrams.
		return &Result{Wildcard: true}
	}
}
