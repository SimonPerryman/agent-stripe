package output

import (
	"fmt"
	"strings"
)

// Closest returns the candidate nearest to input by Levenshtein distance,
// case-insensitive, within max(2, len(input)/3). Returns "" if nothing
// qualifies or input is empty.
func Closest(input string, candidates []string) string {
	if input == "" || len(candidates) == 0 {
		return ""
	}
	limit := len(input) / 3
	if limit < 2 {
		limit = 2
	}
	lower := strings.ToLower(input)
	best := ""
	bestDist := limit + 1
	for _, c := range candidates {
		d := levenshtein(lower, strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if bestDist > limit {
		return ""
	}
	return best
}

// ValidList renders "valid: a, b, c" for use when no close match exists.
func ValidList(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	return fmt.Sprintf("valid: %s", strings.Join(candidates, ", "))
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
