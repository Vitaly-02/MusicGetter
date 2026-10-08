package matcher

import (
	"math"
	"strings"
)

// Restricted Damerau-Levenshtein (adjacent transposition) rune distance uses O(min(n,m)) memory; metadata lengths are domain-bounded.
func textSimilarity(a, b string) float64 {
	if a == b && a != "" {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	x, y := []rune(a), []rune(b)
	// Avoid quadratic work for unusually long (but valid) metadata.
	if len(x) > 256 || len(y) > 256 {
		return bigramSimilarity(x, y)
	}
	if len(x) > len(y) {
		x, y = y, x
	}
	prev2, prev, row := make([]int, len(x)+1), make([]int, len(x)+1), make([]int, len(x)+1)
	for i := range prev {
		prev[i] = i
	}
	for j := 1; j <= len(y); j++ {
		row[0] = j
		for i := 1; i <= len(x); i++ {
			cost := 0
			if x[i-1] != y[j-1] {
				cost = 1
			}
			row[i] = min(prev[i]+1, row[i-1]+1, prev[i-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				row[i] = min(row[i], prev2[i-2]+1)
			}
		}
		prev2, prev, row = prev, row, prev2
	}
	return 1 - float64(prev[len(x)])/float64(len(y))
}
func tokenSimilarity(a, b string) float64 {
	x, y := strings.Fields(a), strings.Fields(b)
	seen := map[string]bool{}
	other := map[string]bool{}
	for _, s := range x {
		seen[s] = true
	}
	for _, s := range y {
		other[s] = true
	}
	if len(seen)+len(other) == 0 {
		return 0
	}
	common := 0
	for s := range seen {
		if other[s] {
			common++
		}
	}
	return 2 * float64(common) / float64(len(seen)+len(other))
}
func nameSimilarity(a, b string) float64 {
	return math.Max(textSimilarity(a, b), tokenSimilarity(a, b))
}

// Symmetric best-match coverage penalizes missing/extra featured artists. Exact
// name equality is cheap; typo comparisons never use invented alias dictionaries.
func artistSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	coverage := func(x, y []string) float64 {
		sum := 0.0
		for _, artist := range x {
			best := 0.0
			for _, candidate := range y {
				best = math.Max(best, nameSimilarity(artist, candidate))
			}
			sum += best
		}
		return sum / float64(len(x))
	}
	return math.Min(coverage(a, b), coverage(b, a))
}

func bigramSimilarity(a, b []rune) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	counts := map[[2]rune]int{}
	for i := 1; i < len(a); i++ {
		counts[[2]rune{a[i-1], a[i]}]++
	}
	common := 0
	for i := 1; i < len(b); i++ {
		key := [2]rune{b[i-1], b[i]}
		if counts[key] > 0 {
			counts[key]--
			common++
		}
	}
	return 2 * float64(common) / float64(len(a)+len(b)-2)
}
