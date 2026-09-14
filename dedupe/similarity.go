package dedupe

// This file holds the two string-similarity measures the scorer uses. Both are
// implemented here rather than pulled from a library so the solution has no
// third-party dependencies and the behaviour is fully inspectable.

// jaroWinkler returns a similarity in [0,1] for two short strings, favouring
// pairs that agree on their first characters.
//
// It is used for personal names because the errors that appear in names are
// typos and truncations near the end of the word ("Cathy"/"Cathi",
// "Robert"/"Roberto"), which a common-prefix bonus handles well.
func jaroWinkler(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	j := jaro(ra, rb)
	if j < 0.7 {
		// Standard practice: only boost pairs that are already similar, so a
		// shared first letter cannot lift two unrelated names.
		return j
	}
	prefix := 0
	for prefix < min(4, min(len(ra), len(rb))) && ra[prefix] == rb[prefix] {
		prefix++
	}
	const scaling = 0.1
	return j + float64(prefix)*scaling*(1-j)
}

func jaro(a, b []rune) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Two characters may only be matched if they are within this distance of
	// each other, which is what keeps Jaro from pairing far-apart letters.
	window := max(len(a), len(b))/2 - 1
	if window < 0 {
		window = 0
	}

	aMatched := make([]bool, len(a))
	bMatched := make([]bool, len(b))
	matches := 0
	for i := range a {
		lo := max(0, i-window)
		hi := min(len(b)-1, i+window)
		for j := lo; j <= hi; j++ {
			if bMatched[j] || a[i] != b[j] {
				continue
			}
			aMatched[i], bMatched[j] = true, true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	// Count matched characters that appear in a different relative order.
	transpositions, k := 0, 0
	for i := range a {
		if !aMatched[i] {
			continue
		}
		for !bMatched[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	t := float64(transpositions) / 2
	return (m/float64(len(a)) + m/float64(len(b)) + (m-t)/m) / 3
}

// levenshteinRatio returns 1 - editDistance/maxLen, a similarity in [0,1].
//
// It is used for street addresses, where differences are insertions and
// deletions spread through a long string ("Ap #312-8611 Lacus Ave" vs
// "312-8611 Lacus Ave") and a prefix bonus would be misleading.
func levenshteinRatio(a, b string) float64 {
	if a == "" && b == "" {
		return 1
	}
	longest := max(len([]rune(a)), len([]rune(b)))
	if longest == 0 {
		return 1
	}
	return 1 - float64(levenshtein(a, b))/float64(longest)
}

// levenshtein computes edit distance with the usual two-row rolling buffer,
// which keeps memory at O(min(len)) instead of O(len*len).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra // iterate over the shorter string in the inner loop
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}
